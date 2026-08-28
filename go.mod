module aegis

go 1.26

require (
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.8.0
	github.com/alibabacloud-go/darabonba-openapi/v2 v2.2.4
	github.com/alibabacloud-go/dm-20151123/v2 v2.11.1
	github.com/alibabacloud-go/dysmsapi-20170525/v5 v5.6.0
	github.com/alibabacloud-go/tea v1.5.3
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/aliyun/aliyun-oss-go-sdk v3.0.2+incompatible
	github.com/aliyun/credentials-go v1.4.12
	github.com/anthropics/anthropic-sdk-go v1.68.0
	github.com/aws/aws-sdk-go-v2 v1.43.6
	github.com/aws/aws-sdk-go-v2/config v1.32.37
	github.com/aws/aws-sdk-go-v2/credentials v1.19.36
	github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager v0.3.11
	github.com/aws/aws-sdk-go-v2/service/s3 v1.107.0
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.66.5
	github.com/aws/smithy-go v1.27.8
	github.com/bbrks/go-blurhash v1.1.1
	github.com/blevesearch/bleve/v2 v2.6.0
	github.com/boombuler/barcode v1.1.0
	github.com/bsm/redislock v0.10.0
	github.com/casbin/casbin/v2 v2.135.0
	github.com/clbanning/mxj/v2 v2.7.0
	github.com/cloudwego/eino v0.9.15
	github.com/common-nighthawk/go-figure v0.0.0-20210622060536-734e95fb86be
	github.com/corazawaf/coraza-coreruleset/v4 v4.25.0
	github.com/corazawaf/coraza/v3 v3.7.0
	github.com/coreos/go-oidc/v3 v3.20.0
	github.com/crewjam/saml v0.5.1
	github.com/dchest/captcha v1.1.0
	github.com/disintegration/imaging v1.6.2
	github.com/dop251/goja v0.0.0-20260806115107-493f22071ef6
	github.com/dop251/goja_nodejs v0.0.0-20260212111938-1f56ff5bcf14
	github.com/dustin/go-humanize v1.0.1
	github.com/eino-contrib/jsonschema v1.0.3
	github.com/ethereum/go-ethereum v1.17.5
	github.com/expr-lang/expr v1.17.8
	github.com/fogleman/gg v1.3.0
	github.com/fsnotify/fsnotify v1.10.1
	github.com/gabriel-vasile/mimetype v1.4.13
	github.com/getkin/kin-openapi v0.146.0
	github.com/gin-contrib/cors v1.7.7
	github.com/gin-gonic/gin v1.12.0
	github.com/go-ldap/ldap/v3 v3.4.14
	github.com/go-playground/validator/v10 v10.30.3
	github.com/go-redis/redis_rate/v10 v10.0.1
	github.com/go-resty/resty/v2 v2.17.2
	github.com/go-sql-driver/mysql v1.10.0
	github.com/go-webauthn/webauthn v0.17.4
	github.com/goccy/go-json v0.10.6
	github.com/goccy/go-yaml v1.19.2
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/inbucket/html2text v1.0.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/jedib0t/go-pretty/v6 v6.8.3
	github.com/lucasb-eyer/go-colorful v1.4.0
	github.com/mailgun/mailgun-go/v5 v5.19.1
	github.com/mattn/go-isatty v0.0.24
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/mileusna/useragent v1.3.5
	github.com/minio/minio-go/v7 v7.2.1
	github.com/mojocn/base64Captcha v1.3.8
	github.com/mozillazg/go-pinyin v0.21.0
	github.com/nats-io/nats.go v1.52.0
	github.com/openai/openai-go/v3 v3.54.0
	github.com/oschwald/geoip2-golang v1.13.0
	github.com/panjf2000/ants/v2 v2.12.1
	github.com/paulmach/orb v0.13.0
	github.com/plutov/paypal/v4 v4.17.0
	github.com/pquerna/otp v1.5.0
	github.com/qiniu/go-sdk/v7 v7.27.0
	github.com/realclientip/realclientip-go v1.0.0
	github.com/redis/go-redis/v9 v9.22.0
	github.com/resend/resend-go/v2 v2.28.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/russellhaering/goxmldsig v1.6.1
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2
	github.com/schollz/progressbar/v3 v3.19.1
	github.com/secure-io/sio-go v0.3.1
	github.com/sendgrid/sendgrid-go v3.16.1+incompatible
	github.com/shadowsocks/go-shadowsocks2 v0.1.5
	github.com/shirou/gopsutil/v4 v4.26.7
	github.com/shopspring/decimal v1.4.0
	github.com/signintech/gopdf v0.38.0
	github.com/smartwalle/alipay/v3 v3.2.31
	github.com/sony/gobreaker/v2 v2.4.0
	github.com/spf13/viper v1.21.0
	github.com/stripe/stripe-go/v82 v82.5.1
	github.com/studio-b12/gowebdav v0.13.0
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common v1.3.157
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses v1.3.136
	github.com/tencentyun/cos-go-sdk-v5 v0.7.75
	github.com/tetratelabs/wazero v1.12.0
	github.com/things-go/go-socks5 v0.1.1
	github.com/tidwall/gjson v1.19.0
	github.com/trustelem/zxcvbn v1.0.1
	github.com/ulule/limiter/v3 v3.11.2
	github.com/vanng822/go-premailer v1.35.0
	github.com/wechatpay-apiv3/wechatpay-go v0.2.21
	github.com/wneessen/go-mail v0.8.1
	github.com/xuri/excelize/v2 v2.11.0
	go.opentelemetry.io/otel v1.45.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.45.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.45.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.45.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.45.0
	go.opentelemetry.io/otel/sdk v1.45.0
	go.opentelemetry.io/otel/trace v1.45.0
	go.temporal.io/api v1.63.5
	go.temporal.io/sdk v1.47.0
	go.uber.org/fx v1.24.0
	go.uber.org/zap v1.28.0
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba
	golang.org/x/crypto v0.54.0
	golang.org/x/image v0.44.0
	golang.org/x/net v0.57.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/sync v0.22.0
	golang.org/x/term v0.45.0
	golang.org/x/text v0.40.0
	golang.org/x/time v0.15.0
	golang.org/x/tools v0.47.0
	google.golang.org/api v0.292.0
	google.golang.org/genai v1.70.0
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.22.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.23.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.12.0 // indirect
	github.com/Azure/go-ntlmssp v0.1.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime v0.0.0-20260311194731-d5b7577c683d // indirect
	github.com/PuerkitoBio/goquery v1.12.0 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.16.0 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/alex-ant/gomath v0.0.0-20160516115720-89013a210a82 // indirect
	github.com/alibabacloud-go/alibabacloud-gateway-spi v0.0.5 // indirect
	github.com/alibabacloud-go/debug v1.0.1 // indirect
	github.com/alibabacloud-go/tea-utils/v2 v2.0.9 // indirect
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.16 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.37 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.37 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.37 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.38 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.28 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.37 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.36 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.6 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/beevik/etree v1.7.0 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/blevesearch/bleve_index_api v1.3.11 // indirect
	github.com/blevesearch/geo v0.2.5 // indirect
	github.com/blevesearch/go-faiss v1.1.0 // indirect
	github.com/blevesearch/go-porterstemmer v1.0.3 // indirect
	github.com/blevesearch/gtreap v0.1.1 // indirect
	github.com/blevesearch/mmap-go v1.2.0 // indirect
	github.com/blevesearch/scorch_segment_api/v2 v2.4.7 // indirect
	github.com/blevesearch/segment v0.9.1 // indirect
	github.com/blevesearch/snowballstem v0.9.0 // indirect
	github.com/blevesearch/upsidedown_store_api v1.0.2 // indirect
	github.com/blevesearch/vellum v1.2.0 // indirect
	github.com/blevesearch/zapx/v11 v11.4.3 // indirect
	github.com/blevesearch/zapx/v12 v12.4.3 // indirect
	github.com/blevesearch/zapx/v13 v13.4.3 // indirect
	github.com/blevesearch/zapx/v14 v14.4.3 // indirect
	github.com/blevesearch/zapx/v15 v15.4.3 // indirect
	github.com/blevesearch/zapx/v16 v16.3.4 // indirect
	github.com/blevesearch/zapx/v17 v17.1.2 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/bytedance/gopkg v0.1.4 // indirect
	github.com/bytedance/sonic v1.15.0 // indirect
	github.com/bytedance/sonic/loader v0.5.1 // indirect
	github.com/casbin/govaluate v1.10.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clbanning/mxj v1.8.4 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/consensys/gnark-crypto v0.20.1 // indirect
	github.com/corazawaf/libinjection-go v0.3.2 // indirect
	github.com/crate-crypto/go-eth-kzg v1.5.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/dop251/base64dec v0.0.0-20231022112746-c6c9f9a96217 // indirect
	github.com/ebitengine/purego v0.10.2 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.8 // indirect
	github.com/facebookgo/clock v0.0.0-20150410010913-600d898af40a // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fjl/jsonw v0.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.2 // indirect
	github.com/gin-contrib/sse v1.1.1 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.2.6 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/snappy v1.0.1-0.20260716114414-9ae09f520e93 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/pprof v0.0.0-20240727154555-813a5fbdbec8 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.19 // indirect
	github.com/googleapis/gax-go/v2 v2.23.0 // indirect
	github.com/goph/emperror v0.17.2 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.2 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kaptinlin/go-i18n v0.2.12 // indirect
	github.com/kaptinlin/jsonpointer v0.4.17 // indirect
	github.com/kaptinlin/jsonschema v0.7.6 // indirect
	github.com/kaptinlin/messageformat-go v0.4.18 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/klauspost/crc32 v1.3.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/magefile/mage v1.17.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/minio/crc64nvme v1.1.1 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mozillazg/go-httpheader v0.4.0 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/nexus-rpc/nexus-proto-annotations v0.1.0 // indirect
	github.com/nexus-rpc/sdk-go v0.6.0 // indirect
	github.com/nikolalohinski/gonja v1.5.3 // indirect
	github.com/oapi-codegen/runtime v1.6.0 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/olekukonko/errors v1.1.0 // indirect
	github.com/olekukonko/ll v0.0.9 // indirect
	github.com/olekukonko/tablewriter v1.0.7 // indirect
	github.com/oschwald/maxminddb-golang v1.13.1 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/pelletier/go-toml/v2 v2.3.1 // indirect
	github.com/petar-dambovaliev/aho-corasick v0.0.0-20250424160509-463d218d4745 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/phpdave11/gofpdi v1.0.14-0.20211212211723-1f10f9844311 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.59.1 // indirect
	github.com/richardlehane/mscfb v1.0.7 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/riobard/go-bloom v0.0.0-20200614022211-cdc8013cb5b3 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/sendgrid/rest v2.6.9+incompatible // indirect
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/slongfield/pyfmt v0.0.0-20220222012616-ea85ff4c361f // indirect
	github.com/smartwalle/ncrypto v1.0.4 // indirect
	github.com/smartwalle/ngx v1.1.2 // indirect
	github.com/smartwalle/nsign v1.0.9 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/ssor/bom v0.0.0-20170718123548-6386211fdfcf // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/supranational/blst v0.3.16 // indirect
	github.com/test-go/testify v1.1.4 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	github.com/valllabh/ocsf-schema-golang v1.0.3 // indirect
	github.com/vanng822/css v1.0.1 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	github.com/yargevad/filepathx v1.0.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	go.mongodb.org/mongo-driver/v2 v2.5.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/dig v1.19.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/arch v0.25.0 // indirect
	golang.org/x/exp v0.0.0-20240404231335-c0f41cb1a7a0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/grpc v1.83.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/ini.v1 v1.67.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/fileutil v1.4.0 // indirect
	rsc.io/binaryregexp v0.2.0 // indirect
)
