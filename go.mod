module github.com/stackgenhq/genie

go 1.25.6

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/adhocore/gronx v1.19.6
	github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260201021820-c2d2db22a1c9
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de
	github.com/aragossa/pii-shield v1.2.0
	github.com/bwmarrin/discordgo v0.29.0
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/charmbracelet/huh v0.8.0
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/chromedp/chromedp v0.14.2
	github.com/dominikbraun/graph v0.23.0
	github.com/drone/go-scm v0.0.0-00010101000000-000000000000
	github.com/dslipak/pdf v0.0.2
	github.com/emersion/go-imap v1.2.1
	github.com/emersion/go-message v0.18.2
	github.com/expr-lang/expr v1.17.8
	github.com/fsnotify/fsnotify v1.9.0
	github.com/go-chi/chi/v5 v5.2.5
	github.com/go-telegram/bot v1.19.0
	github.com/google/uuid v1.6.0
	github.com/hashicorp/go-retryablehttp v0.7.8
	github.com/infracloudio/msbotbuilder-go v0.2.5
	github.com/klauspost/compress v1.18.0
	github.com/mark3labs/mcp-go v0.44.0
	github.com/milvus-io/milvus/client/v2 v2.6.2
	github.com/onsi/ginkgo/v2 v2.28.1
	github.com/onsi/gomega v1.39.1
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/slack-go/slack v0.18.0
	github.com/sony/gobreaker/v2 v2.4.0
	github.com/spf13/cobra v1.10.2
	github.com/tidwall/gjson v1.18.0
	github.com/tidwall/sjson v1.2.5
	github.com/zalando/go-keyring v0.2.6
	go.mau.fi/whatsmeow v0.0.0-20260211193157-7b33f6289f98
	go.opentelemetry.io/otel v1.40.0
	go.opentelemetry.io/otel/metric v1.40.0
	go.opentelemetry.io/otel/trace v1.40.0
	gocloud.dev v0.44.0
	golang.org/x/net v0.51.0
	golang.org/x/oauth2 v0.35.0
	golang.org/x/sync v0.19.0
	golang.org/x/time v0.14.0
	google.golang.org/api v0.256.0
	google.golang.org/genai v1.47.0
	google.golang.org/grpc v1.79.1
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.1
	modernc.org/sqlite v1.46.1
	trpc.group/trpc-go/trpc-agent-go v1.5.1-0.20260217140857-7458387542dc
	trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/gemini v1.5.1-0.20260217140857-7458387542dc
	trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/huggingface v1.5.1-0.20260217140857-7458387542dc
	trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/milvus v1.5.1-0.20260224093107-42ba37309edf
	trpc.group/trpc-go/trpc-agent-go/model/anthropic v1.6.0
	trpc.group/trpc-go/trpc-agent-go/model/gemini v1.5.1-0.20260213075829-fd2e4a85e54f
	trpc.group/trpc-go/trpc-agent-go/model/ollama v1.5.0
	trpc.group/trpc-go/trpc-agent-go/tool/google v1.5.1-0.20260213075829-fd2e4a85e54f
)

require (
	al.essio.dev/pkg/shellescape v1.5.1 // indirect
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.17.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.5.3 // indirect
	cloud.google.com/go/secretmanager v1.16.0 // indirect
	filippo.io/edwards25519 v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/anthropics/anthropic-sdk-go v1.22.1 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aws/aws-sdk-go-v2 v1.39.6 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.31.17 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.18.21 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.35.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.39.1 // indirect
	github.com/aws/smithy-go v1.23.2 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/beeper/argo-go v1.1.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/catppuccin/go v0.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/bubbles v0.21.1-0.20250623103423-23b8fd6302d7 // indirect
	github.com/charmbracelet/bubbletea v1.3.6 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/x/ansi v0.9.3 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13 // indirect
	github.com/charmbracelet/x/exp/strings v0.0.0-20240722160745-212f7b056ed0 // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/chromedp/cdproto v0.0.0-20250724212937-08a3db8b4327 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/cilium/ebpf v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cockroachdb/errors v1.9.1 // indirect
	github.com/cockroachdb/logtags v0.0.0-20211118104740-dabe8e521a4f // indirect
	github.com/cockroachdb/redact v1.1.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/containerd/cgroups/v3 v3.0.3 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/danieljoos/wincred v1.2.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/elliotchance/orderedmap/v3 v3.1.0 // indirect
	github.com/emersion/go-sasl v0.0.0-20200509203442-7bfe0ed36a21 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/getsentry/sentry-go v0.12.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20250725192818-e39067aee2d2 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260115054156-294ebfa9ad83 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/wire v0.7.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.12 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.0.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.1 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/json-iterator/go v1.1.13-0.20220915233716-71ac16282d12 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lestrrat-go/backoff/v2 v2.0.8 // indirect
	github.com/lestrrat-go/blackmagic v1.0.2 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/jwx v1.2.29 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.20 // indirect
	github.com/mattn/go-sqlite3 v1.14.33 // indirect
	github.com/maxbrunsfeld/counterfeiter/v6 v6.12.1-0.20251113130158-fd93bc43410d // indirect
	github.com/milvus-io/milvus-proto/go-api/v2 v2.6.8-0.20251223041313-25746c47c1a7 // indirect
	github.com/milvus-io/milvus/pkg/v2 v2.6.7-0.20251201120310-af64f2acba38 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/ollama/ollama v0.13.0 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/opencontainers/runtime-spec v1.0.2 // indirect
	github.com/panjf2000/ants/v2 v2.11.3 // indirect
	github.com/petermattis/goid v0.0.0-20260113132338-7c7de50cc741 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.4 // indirect
	github.com/prometheus/procfs v0.19.2 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/samber/lo v1.27.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/shirou/gopsutil/v3 v3.23.12 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20201229170055-e5319fda7802 // indirect
	github.com/twpayne/go-geom v1.6.1 // indirect
	github.com/uber/jaeger-client-go v2.30.0+incompatible // indirect
	github.com/vektah/gqlparser/v2 v2.5.27 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.4.13 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	go.etcd.io/etcd/api/v3 v3.6.8 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.8 // indirect
	go.etcd.io/etcd/client/v3 v3.6.8 // indirect
	go.etcd.io/etcd/pkg/v3 v3.6.8 // indirect
	go.etcd.io/etcd/server/v3 v3.6.8 // indirect
	go.etcd.io/raft/v3 v3.6.0 // indirect
	go.mau.fi/libsignal v0.2.1 // indirect
	go.mau.fi/util v0.9.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.62.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.65.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.37.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.37.0 // indirect
	go.opentelemetry.io/otel/sdk v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.40.0 // indirect
	go.opentelemetry.io/proto/otlp v1.7.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/automaxprocs v1.5.3 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto v0.0.0-20250715232539-7130f93afb79 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260202165425-ce8ad4cf556b // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260209200024-4cfbd4190f57 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apimachinery v0.32.3 // indirect
	modernc.org/libc v1.67.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
	trpc.group/trpc-go/trpc-a2a-go v0.2.5 // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/milvus v0.0.0-20251216100013-73147c973dde // indirect
)

tool (
	github.com/maxbrunsfeld/counterfeiter/v6
	github.com/onsi/ginkgo/v2/ginkgo
)

replace github.com/drone/go-scm => github.com/appcd-dev/go-scm v0.0.0-20251204195827-1d9c5431af88

replace trpc.group/trpc-go/trpc-agent-go => github.com/sks/trpc-agent-go v0.0.0-20260301180246-c5e2a2e4bde6

replace trpc.group/trpc-go/trpc-agent-go/model/gemini => github.com/sks/trpc-agent-go/model/gemini v0.0.0-20260222042814-5a8ec2571a6f
