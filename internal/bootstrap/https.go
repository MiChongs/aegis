package bootstrap

import (
	"errors"
	"fmt"
	"net/http"

	"aegis/internal/config"
	"aegis/pkg/httpserve"

	"go.uber.org/zap"
	"golang.org/x/crypto/acme/autocert"
)

// Serve 启动对外监听：明文 HTTP_PORT 总是开着（回源、容器健康检查、内网调用走它），
// TLS_ENABLED 时再在 HTTPS_PORT 上开 HTTPS（TCP，h2 + http/1.1）与 HTTP/3（UDP，同端口号）。
//
// 端口是同步绑定的：证书读不出来、端口被占，这里直接返回错误让启动失败；
// 跑起来之后的异常退出走 onError。
func (a *APIApp) Serve(onError func(error)) error {
	var acme *autocert.Manager
	if a.Config.TLSServer.Enabled {
		mgr, err := a.startSecureServer(onError)
		if err != nil {
			return err
		}
		acme = mgr
	}
	a.Server.Handler = plainHandler(a.Config.TLSServer, a.Server.Handler, acme)
	go func() {
		if err := a.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			onError(fmt.Errorf("api serve failed: %w", err))
		}
	}()
	return nil
}

func (a *APIApp) startSecureServer(onError func(error)) (*autocert.Manager, error) {
	tc := a.Config.TLSServer
	bundle, err := httpserve.BuildTLSConfig(httpserve.CertSource{
		CertFile:             tc.CertFile,
		KeyFile:              tc.KeyFile,
		AutocertDomains:      tc.AutocertDomains,
		AutocertCacheDir:     tc.AutocertCacheDir,
		AutocertEmail:        tc.AutocertEmail,
		AutocertDirectoryURL: tc.AutocertDirectoryURL,
	}, tc.MinVersion)
	if err != nil {
		return nil, err
	}
	log := a.Logger.Named("https")
	if bundle.Reload != nil {
		bundle.Reload.OnReload(func(err error) {
			if err != nil {
				log.Warn("tls certificate reload failed, keeping the previous certificate", zap.Error(err))
				return
			}
			log.Info("tls certificate reloaded", zap.String("cert", tc.CertFile))
		})
	}

	opts := httpserve.Options{
		Addr:              fmt.Sprintf(":%d", tc.Port),
		TLS:               bundle.Config,
		Handler:           a.drain.wrap(a.Router),
		HSTS:              tc.HSTSHeader(),
		ReadHeaderTimeout: a.Server.ReadHeaderTimeout,
		ReadTimeout:       a.Server.ReadTimeout,
		WriteTimeout:      a.Server.WriteTimeout,
		IdleTimeout:       a.Server.IdleTimeout,
		// 扫描器的半截握手每天成千上万，放在 Debug 级别，排查握手问题时再打开
		ErrorLog: zap.NewStdLog(log.WithOptions(zap.IncreaseLevel(zap.DebugLevel))),
	}
	if tc.HTTP3.Enabled {
		opts.HTTP3Addr = fmt.Sprintf(":%d", tc.HTTP3.Port)
		opts.AltSvcPort = tc.HTTP3.PublicPort
		opts.AltSvcMaxAge = tc.HTTP3.AltSvcMaxAge
		opts.Allow0RTT = tc.HTTP3.Allow0RTT
		opts.QUIC = httpserve.QUICOptions{
			MaxIdleTimeout:     tc.HTTP3.MaxIdleTimeout,
			KeepAlivePeriod:    tc.HTTP3.KeepAlivePeriod,
			MaxIncomingStreams: tc.HTTP3.MaxStreams,
		}
	}
	secure, err := httpserve.New(opts)
	if err != nil {
		return nil, err
	}
	if err := secure.Listen(); err != nil {
		return nil, err
	}
	a.Secure = secure
	secure.Serve(onError)

	fields := []zap.Field{zap.Int("https_port", tc.Port), zap.Bool("http3", secure.HTTP3Enabled())}
	if secure.HTTP3Enabled() {
		fields = append(fields, zap.Int("http3_port", tc.HTTP3.Port), zap.String("alt_svc", secure.AltSvc()))
	}
	if tc.UsesAutocert() {
		fields = append(fields, zap.Strings("autocert_domains", tc.AutocertDomains))
	}
	log.Info("https listener started", fields...)
	return bundle.Manager, nil
}

// plainHandler 明文 HTTP_PORT 上的处理链：ACME HTTP-01 质询 → 跳转 HTTPS（可选）→ 路由。
// 质询必须由签发证书的同一个 Manager 来答，所以从 HTTPS 那边把它传过来。
func plainHandler(tc config.TLSServerConfig, next http.Handler, acme *autocert.Manager) http.Handler {
	if !tc.Enabled {
		return next
	}
	handler := next
	if tc.RedirectHTTP {
		handler = httpserve.RedirectToHTTPS(tc.PublicPort, isProbePath, handler)
	}
	if acme != nil {
		handler = acme.HTTPHandler(handler)
	}
	return handler
}

func isProbePath(path string) bool {
	return path == "/healthz" || path == "/readyz"
}
