package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	geoip2 "github.com/oschwald/geoip2-golang"
	"go.uber.org/zap"
)

type geoDBMeta struct {
	LastUpdatedAt time.Time `json:"lastUpdatedAt"`
}

type geoDatabaseResolver struct {
	log      *zap.Logger
	cfg      config.GeoIPConfig
	http     *http.Client
	baseDir  string
	cityPath string
	asnPath  string
	metaPath string

	mu     sync.RWMutex
	city   *geoip2.Reader
	asn    *geoip2.Reader
	cancel context.CancelFunc
	done   chan struct{}
}

func newGeoDatabaseResolver(log *zap.Logger, cfg config.GeoIPConfig) (*geoDatabaseResolver, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if log == nil {
		log = zap.NewNop()
	}

	baseDir := strings.TrimSpace(cfg.DatabaseDir)
	if baseDir == "" {
		baseDir = ".runtime/geoip"
	}
	baseDir = filepath.Clean(baseDir)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create geoip database dir: %w", err)
	}

	resolver := &geoDatabaseResolver{
		log:      log,
		cfg:      cfg,
		http:     &http.Client{Timeout: cfg.DownloadTimeout},
		baseDir:  baseDir,
		cityPath: filepath.Join(baseDir, "GeoLite2-City.mmdb"),
		asnPath:  filepath.Join(baseDir, "GeoLite2-ASN.mmdb"),
		metaPath: filepath.Join(baseDir, "meta.json"),
		done:     make(chan struct{}),
	}
	if err := resolver.loadReaders(); err != nil {
		resolver.log.Warn("load geoip database readers failed", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	resolver.cancel = cancel
	go resolver.run(ctx)
	return resolver, nil
}

func (r *geoDatabaseResolver) run(ctx context.Context) {
	defer close(r.done)

	if r.needsRefresh() {
		r.refreshDatabases(ctx)
	}

	ticker := time.NewTicker(r.cfg.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshDatabases(ctx)
		}
	}
}

func (r *geoDatabaseResolver) Close() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.done != nil {
		<-r.done
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.city != nil {
		_ = r.city.Close()
		r.city = nil
	}
	if r.asn != nil {
		_ = r.asn.Close()
		r.asn = nil
	}
}

func (r *geoDatabaseResolver) Lookup(ip string) (IPLocation, error) {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return IPLocation{}, fmt.Errorf("invalid ip")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var (
		country      string
		countryCode  string
		region       string
		city         string
		district     string
		timezone     string
		isp          string
		organization string
		asn          string
		coordinates  *GeoCoordinate
	)

	if r.city != nil {
		record, err := r.city.City(parsed)
		if err != nil {
			return IPLocation{}, err
		}
		if record != nil {
			country = localizedGeoName(record.Country.Names)
			countryCode = strings.ToUpper(strings.TrimSpace(record.Country.IsoCode))
			if len(record.Subdivisions) > 0 {
				region = localizedGeoName(record.Subdivisions[0].Names)
			}
			if len(record.Subdivisions) > 1 {
				district = localizedGeoName(record.Subdivisions[1].Names)
			}
			city = localizedGeoName(record.City.Names)
			timezone = normalizeTimezone(country, record.Location.TimeZone)
			if record.Location.Latitude != 0 || record.Location.Longitude != 0 {
				lat := record.Location.Latitude
				lng := record.Location.Longitude
				coordinates = &GeoCoordinate{Latitude: &lat, Longitude: &lng}
			}
		}
	}

	if r.asn != nil {
		record, err := r.asn.ASN(parsed)
		if err != nil {
			return IPLocation{}, err
		}
		if record != nil {
			organization = normalizeString(record.AutonomousSystemOrganization)
			isp = normalizeISP(record.AutonomousSystemOrganization)
			if record.AutonomousSystemNumber > 0 {
				asn = fmt.Sprintf("AS%d", record.AutonomousSystemNumber)
			}
		}
	}

	displayCountry, normalizedCountryCode := normalizeCountry(country, countryCode)
	loc := IPLocation{
		IP:          strings.TrimSpace(ip),
		Country:     displayCountry,
		CountryCode: normalizedCountryCode,
		Region:      normalizeString(region),
		City:        normalizeString(city),
		District:    normalizeString(district),
		Location:    composeLocation(displayCountry, region, city, district),
		Timezone:    timezone,
		ISP:         isp,
		Coordinates: coordinates,
		Network: NetworkInfo{
			Type:         detectNetworkType(organization),
			Organization: organization,
			ASN:          normalizeString(asn),
		},
		Source:     "geoip-mmdb",
		ResolvedAt: time.Now().UTC(),
	}
	if !loc.isResolved() {
		return IPLocation{}, fmt.Errorf("geoip database returned empty result")
	}
	return loc, nil
}

func (r *geoDatabaseResolver) needsRefresh() bool {
	if fileMissing(r.cityPath) || fileMissing(r.asnPath) {
		return true
	}
	meta, err := r.readMeta()
	if err == nil && !meta.LastUpdatedAt.IsZero() {
		return time.Since(meta.LastUpdatedAt) >= r.cfg.UpdateInterval
	}
	info, err := os.Stat(r.cityPath)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) >= r.cfg.UpdateInterval
}

func (r *geoDatabaseResolver) refreshDatabases(ctx context.Context) {
	var (
		cityTemp string
		asnTemp  string
		updated  bool
	)

	if strings.TrimSpace(r.cfg.CityDBURL) != "" {
		if fileMissing(r.cityPath) || r.needsRefresh() {
			path, err := r.downloadDatabase(ctx, r.cfg.CityDBURL, "city")
			if err != nil {
				r.log.Warn("refresh geoip city database failed", zap.Error(err))
			} else {
				cityTemp = path
				updated = true
			}
		}
	}

	if strings.TrimSpace(r.cfg.ASNDBURL) != "" {
		if fileMissing(r.asnPath) || r.needsRefresh() {
			path, err := r.downloadDatabase(ctx, r.cfg.ASNDBURL, "asn")
			if err != nil {
				r.log.Warn("refresh geoip asn database failed", zap.Error(err))
			} else {
				asnTemp = path
				updated = true
			}
		}
	}

	if !updated {
		return
	}
	defer cleanupTempFile(cityTemp)
	defer cleanupTempFile(asnTemp)

	if err := r.promote(cityTemp, asnTemp); err != nil {
		r.log.Warn("promote geoip databases failed", zap.Error(err))
		return
	}
	if err := r.writeMeta(geoDBMeta{LastUpdatedAt: time.Now().UTC()}); err != nil {
		r.log.Warn("write geoip metadata failed", zap.Error(err))
	}
}

func (r *geoDatabaseResolver) downloadDatabase(ctx context.Context, rawURL, prefix string) (string, error) {
	requestURL := r.optimizedURL(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aegis-geoip-updater/1.0")

	resp, err := r.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("download %s failed with status %d", prefix, resp.StatusCode)
	}

	tempFile, err := os.CreateTemp(r.baseDir, prefix+"-*.mmdb")
	if err != nil {
		return "", err
	}
	tempPath := tempFile.Name()
	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		_ = tempFile.Close()
		cleanupTempFile(tempPath)
		return "", err
	}
	if err := tempFile.Close(); err != nil {
		cleanupTempFile(tempPath)
		return "", err
	}
	reader, err := geoip2.Open(tempPath)
	if err != nil {
		cleanupTempFile(tempPath)
		return "", fmt.Errorf("validate %s database: %w", prefix, err)
	}
	_ = reader.Close()
	return tempPath, nil
}

func (r *geoDatabaseResolver) promote(cityTemp, asnTemp string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.city != nil {
		_ = r.city.Close()
		r.city = nil
	}
	if r.asn != nil {
		_ = r.asn.Close()
		r.asn = nil
	}

	if cityTemp != "" {
		if err := replaceFile(cityTemp, r.cityPath); err != nil {
			return err
		}
	}
	if asnTemp != "" {
		if err := replaceFile(asnTemp, r.asnPath); err != nil {
			return err
		}
	}

	if fileExists(r.cityPath) {
		reader, err := geoip2.Open(r.cityPath)
		if err != nil {
			return err
		}
		r.city = reader
	}
	if fileExists(r.asnPath) {
		reader, err := geoip2.Open(r.asnPath)
		if err != nil {
			if r.city != nil {
				_ = r.city.Close()
				r.city = nil
			}
			return err
		}
		r.asn = reader
	}
	return nil
}

func (r *geoDatabaseResolver) loadReaders() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.city != nil {
		_ = r.city.Close()
		r.city = nil
	}
	if r.asn != nil {
		_ = r.asn.Close()
		r.asn = nil
	}

	if fileExists(r.cityPath) {
		reader, err := geoip2.Open(r.cityPath)
		if err != nil {
			return err
		}
		r.city = reader
	}
	if fileExists(r.asnPath) {
		reader, err := geoip2.Open(r.asnPath)
		if err != nil {
			if r.city != nil {
				_ = r.city.Close()
				r.city = nil
			}
			return err
		}
		r.asn = reader
	}
	return nil
}

func (r *geoDatabaseResolver) optimizedURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if !r.cfg.ChinaOptimized {
		return raw
	}
	mirror := strings.TrimSpace(r.cfg.GitHubMirror)
	if mirror == "" {
		return raw
	}
	if strings.HasPrefix(raw, mirror) {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if !strings.Contains(host, "github.com") && !strings.Contains(host, "githubusercontent.com") {
		return raw
	}
	return strings.TrimRight(mirror, "/") + "/" + strings.TrimLeft(raw, "/")
}

func (r *geoDatabaseResolver) readMeta() (geoDBMeta, error) {
	data, err := os.ReadFile(r.metaPath)
	if err != nil {
		return geoDBMeta{}, err
	}
	var meta geoDBMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return geoDBMeta{}, err
	}
	return meta, nil
}

func (r *geoDatabaseResolver) writeMeta(meta geoDBMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.metaPath, data, 0o644)
}

func localizedGeoName(names map[string]string) string {
	for _, key := range []string{"zh-CN", "zh", "en"} {
		if value := normalizeChineseGeoName(names[key]); strings.TrimSpace(value) != "" {
			return value
		}
	}
	for _, value := range names {
		if value = normalizeChineseGeoName(value); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeChineseGeoName(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "china":
		return "中国"
	case "hong kong", "hong kong sar":
		return "中国香港"
	case "macau", "macao", "macao sar":
		return "中国澳门"
	case "taiwan":
		return "中国台湾"
	default:
		return value
	}
}

func replaceFile(src, dst string) error {
	if src == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, input, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

func cleanupTempFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.Remove(path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return err != nil
}
