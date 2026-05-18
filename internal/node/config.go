// Package node 实现 node 的配置体系：内置默认 < 配置文件 < 环境变量，
// 角色开关与已启用角色的必填 secret fail-fast 校验。
// 对应 docs/design/server/server.md「配置」章；供 cmd/node 加载与角色分发使用。
package node

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	defaultConfigPath = "./node.yaml"
	configPathEnv     = "NODE_CONFIG"
	envPrefix         = "TSSNODE_"
	envNestSep        = "__"
)

// Config 是 node 的完整配置（log/metrics/relay/coord），字段与 server.md 配置章一致。
type Config struct {
	Log     LogConfig     `yaml:"log"`
	Metrics MetricsConfig `yaml:"metrics"`
	Relay   RelayConfig   `yaml:"relay"`
	Coord   CoordConfig   `yaml:"coord"`
}

// LogConfig 控制日志级别与格式。
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// MetricsConfig 是健康检查与指标监听地址（不含载荷）。
type MetricsConfig struct {
	Listen string `yaml:"listen"`
}

// RelayConfig 是 relay 角色配置（circuit-relay v2 + rendezvous + 访问控制）。
type RelayConfig struct {
	Enable      bool              `yaml:"enable"`
	Listen      []string          `yaml:"listen"`
	PnetPSKRef  string            `yaml:"pnet_psk_ref"`
	TokenVerify TokenVerifyConfig `yaml:"token_verify"`
	Rendezvous  RendezvousConfig  `yaml:"rendezvous"`
	Limits      RelayLimitsConfig `yaml:"limits"`
}

// TokenVerifyConfig 是能力令牌验签公钥来源与自主式信任锚。
type TokenVerifyConfig struct {
	Source       string   `yaml:"source"`
	GroupPubkeys []string `yaml:"group_pubkeys"`
}

// RendezvousConfig 控制 rendezvous 发现开关。
type RendezvousConfig struct {
	Enable bool `yaml:"enable"`
}

// RelayLimitsConfig 是预约/连接/带宽配额（防 DoS）。CircuitMaxDuration 是单条
// circuit-v2 中继链路重置前的最长时长（Go duration，如 "10m"；空=保留 libp2p
// 默认 2m）。security.md 不变量 #10 / RA-001 P1-1：生产联网 keygen/reshare 必带
// Paillier 证明,带证明的 keygen 为分钟级,默认 2m 上限会在 keygen 中途重置链路;
// 故此项须 keygen-aware 放宽/可配,而非靠关证明迁就(testing.md §6)。
type RelayLimitsConfig struct {
	ReservationPerToken int    `yaml:"reservation_per_token"`
	ReservationPerGroup int    `yaml:"reservation_per_group"`
	BandwidthPerConn    string `yaml:"bandwidth_per_conn"`
	CircuitMaxDuration  string `yaml:"circuit_max_duration"`
}

// CoordConfig 是 coord 角色配置（编排者 + 外部业务对接入口）。
type CoordConfig struct {
	Enable   bool                `yaml:"enable"`
	HTTP     CoordHTTPConfig     `yaml:"http"`
	DB       CoordDBConfig       `yaml:"db"`
	External CoordExternalConfig `yaml:"external"`
	Push     CoordPushConfig     `yaml:"push"`
	TTL      CoordTTLConfig      `yaml:"ttl"`
	Quorum   CoordQuorumConfig   `yaml:"quorum"`
	Dispatch CoordDispatchConfig `yaml:"dispatch"`
}

// CoordHTTPConfig 是外部服务 + 成员 API 监听地址。
type CoordHTTPConfig struct {
	Listen string `yaml:"listen"`
}

// CoordDBConfig 是持久化连接串引用（secret）与整库加密开关。
type CoordDBConfig struct {
	DSNRef     string                  `yaml:"dsn_ref"`
	Encryption CoordDBEncryptionConfig `yaml:"encryption"`
}

// CoordDBEncryptionConfig 控制 coord 持久库整库静态加密（database.md §7/§7.1）。
// 默认 enable=true（defaults() 设定）：encrypted + 默认 LOCKED。仅 dev/test 可
// 置 false 禁用——禁用须经生产铁律护栏(Validate)显式非生产确认,否则 node 拒启动。
type CoordDBEncryptionConfig struct {
	Enable bool `yaml:"enable"`
}

// allowInsecureDBEnv 是禁用整库加密时必须显式置位的「非生产确认」env
// (database.md §7.1 生产铁律护栏)。fail-closed：缺省即拒——加固的生产/release
// CI 永不设置它(H-004/H-005 核验),故误在生产禁用加密不可能而非仅不推荐。
const allowInsecureDBEnv = "ALLOW_INSECURE_DB"

// CoordExternalConfig 是外部业务服务鉴权与结果回传方式。
type CoordExternalConfig struct {
	Auth           string `yaml:"auth"`
	APIKeyRef      string `yaml:"api_key_ref"`
	ResultCallback string `yaml:"result_callback"`
}

// CoordPushConfig 是推送凭证引用（secret，可选）。
type CoordPushConfig struct {
	FCMCredRef  string `yaml:"fcm_cred_ref"`
	APNSCredRef string `yaml:"apns_cred_ref"`
}

// CoordTTLConfig 是时钟偏移容差。
type CoordTTLConfig struct {
	SkewTolerance string `yaml:"skew_tolerance"`
}

// CoordQuorumConfig 是签名子集选取策略。
type CoordQuorumConfig struct {
	SignerSelect string `yaml:"signer_select"`
}

// CoordDispatchConfig 是派发后等待签名完成超时。
type CoordDispatchConfig struct {
	Timeout string `yaml:"timeout"`
}

// ErrNoRoleEnabled 表示 relay.enable 与 coord.enable 同为 false 的无效配置。
var ErrNoRoleEnabled = errors.New("relay.enable and coord.enable are both false")

var errSecretMissing = errors.New("required secret missing")

// errSecretPlaintext 表示 secret 项以明文字面值出现：server.md「密钥处理」要求
// 标 secret 的项一律经 env:VAR / file:/path 注入，禁止明文写入提交的配置文件。
var errSecretPlaintext = errors.New("secret must be an env: or file: reference (plaintext forbidden)")

// errInsecureDBNotConfirmed 是生产铁律护栏(database.md §7.1):coord 启用且整库
// 加密被禁用,但未经 ALLOW_INSECURE_DB=1 显式非生产确认 → fail-closed 拒启动。
// 误在生产禁用加密 = 资金编排数据明文落盘的安全红线,护栏使其不可能。
var errInsecureDBNotConfirmed = fmt.Errorf(
	"coord enabled with db.encryption.enable=false but %s=1 not set: "+
		"whole-DB encryption may be disabled only in non-production "+
		"(database.md §7.1 production iron-law guardrail)", allowInsecureDBEnv)

func defaults() Config {
	return Config{
		Log:     LogConfig{Level: "info", Format: "json"},
		Metrics: MetricsConfig{Listen: ":9090"},
		Relay: RelayConfig{
			TokenVerify: TokenVerifyConfig{Source: "config"},
			Rendezvous:  RendezvousConfig{Enable: true},
			Limits: RelayLimitsConfig{
				ReservationPerToken: 4,
				ReservationPerGroup: 8,
				BandwidthPerConn:    "1MiB/s",
				// keygen-aware: a proof-enabled networked keygen/reshare is
				// minutes-long; 10m comfortably covers a small-group session
				// while staying a bounded anti-DoS cap (security.md #10).
				CircuitMaxDuration: "10m",
			},
		},
		Coord: CoordConfig{
			HTTP:     CoordHTTPConfig{Listen: ":8080"},
			DB:       CoordDBConfig{Encryption: CoordDBEncryptionConfig{Enable: true}},
			External: CoordExternalConfig{Auth: "mtls", ResultCallback: "webhook"},
			TTL:      CoordTTLConfig{SkewTolerance: "30s"},
			Quorum:   CoordQuorumConfig{SignerSelect: "liveness"},
			Dispatch: CoordDispatchConfig{Timeout: "120s"},
		},
	}
}

// Load 按「内置默认 < 配置文件 < 环境变量」装配配置。
// 配置文件路径取 NODE_CONFIG，缺省为 ./node.yaml；默认路径不存在时允许纯 env 配置，
// 但 NODE_CONFIG 显式指定的文件缺失即报错。
func Load() (Config, error) {
	cfg := defaults()

	path, explicit := configPath()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist) && !explicit:
		// 默认路径缺省：允许容器/CI 场景纯 env 注入。
	default:
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 执行启动期校验：双角色均关闭即失败；已启用角色的受约束枚举必须合法、
// 必填 secret 必须可解析且为 env:/file: 引用（明文即 fail-fast），可选 secret 一旦
// 配置亦强制引用纪律。校验仅覆盖已启用角色（关闭角色的配置不参与启动校验）。
func (c Config) Validate() error {
	if !c.Relay.Enable && !c.Coord.Enable {
		return ErrNoRoleEnabled
	}
	if c.Relay.Enable {
		if err := validateEnum("relay.token_verify.source", c.Relay.TokenVerify.Source,
			"config", "coord-sync"); err != nil {
			return err
		}
		if _, err := resolveSecret(c.Relay.PnetPSKRef); err != nil {
			return fmt.Errorf("relay enabled: pnet_psk: %w", err)
		}
	}
	if c.Coord.Enable {
		// 生产铁律护栏(database.md §7.1):整库加密被禁用时,必须有显式非生产
		// 确认 ALLOW_INSECURE_DB=1,否则 fail-closed 拒启动。默认 enable=true,
		// 故正常/生产配置不受影响;此分支仅在显式 enable=false 时触发。
		if !c.Coord.DB.Encryption.Enable && os.Getenv(allowInsecureDBEnv) != "1" {
			return errInsecureDBNotConfirmed
		}
		if err := validateEnum("coord.external.auth", c.Coord.External.Auth,
			"mtls", "api_key"); err != nil {
			return err
		}
		if err := validateEnum("coord.external.result_callback", c.Coord.External.ResultCallback,
			"webhook", "longpoll"); err != nil {
			return err
		}
		if err := validateEnum("coord.quorum.signer_select", c.Coord.Quorum.SignerSelect,
			"stable", "liveness"); err != nil {
			return err
		}
		if _, err := resolveSecret(c.Coord.DB.DSNRef); err != nil {
			return fmt.Errorf("coord enabled: db.dsn: %w", err)
		}
		if c.Coord.External.Auth == "api_key" {
			if _, err := resolveSecret(c.Coord.External.APIKeyRef); err != nil {
				return fmt.Errorf("coord enabled (auth=api_key): external.api_key: %w", err)
			}
		}
		if err := optionalSecret(c.Coord.Push.FCMCredRef); err != nil {
			return fmt.Errorf("coord enabled: push.fcm: %w", err)
		}
		if err := optionalSecret(c.Coord.Push.APNSCredRef); err != nil {
			return fmt.Errorf("coord enabled: push.apns: %w", err)
		}
	}
	return nil
}

func configPath() (path string, explicit bool) {
	if p := os.Getenv(configPathEnv); p != "" {
		return p, true
	}
	return defaultConfigPath, false
}

// resolveSecret 解析 secret 引用：仅接受 env:VAR / file:/path；空值视为缺失，
// 任何其它字面值视为明文 secret 并拒绝（server.md 密钥处理：禁明文入提交的配置文件）。
func resolveSecret(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errSecretMissing
	}
	switch {
	case strings.HasPrefix(ref, "env:"):
		v := os.Getenv(strings.TrimPrefix(ref, "env:"))
		if v == "" {
			return "", errSecretMissing
		}
		return v, nil
	case strings.HasPrefix(ref, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(ref, "file:"))
		if err != nil {
			return "", fmt.Errorf("read secret file: %w", err)
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return "", errSecretMissing
		}
		return v, nil
	default:
		return "", errSecretPlaintext
	}
}

// optionalSecret 校验可选 secret：未配置（空引用）即跳过；一旦配置则同样强制
// env:/file: 引用纪律并要求可解析（push 凭证等可选项也禁明文入文件）。
func optionalSecret(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	_, err := resolveSecret(ref)
	return err
}

// validateEnum 校验受约束的字符串取值（server.md 参数表枚举列）。
func validateEnum(field, val string, allowed ...string) error {
	for _, a := range allowed {
		if val == a {
			return nil
		}
	}
	return fmt.Errorf("%s: invalid value %q (allowed: %s)", field, val, strings.Join(allowed, "|"))
}

func applyEnvOverrides(cfg *Config) error {
	return walkEnv(reflect.ValueOf(cfg).Elem(), envPrefix)
}

// walkEnv 递归遍历带 yaml 标签的结构体，按 TSSNODE_<大写键>（嵌套以 __ 连接）覆盖标量。
func walkEnv(v reflect.Value, prefix string) error {
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		key := prefix + strings.ToUpper(name)

		fv := v.Field(i)
		if fv.Kind() == reflect.Struct {
			if err := walkEnv(fv, key+envNestSep); err != nil {
				return err
			}
			continue
		}
		raw, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		if err := setScalar(fv, raw); err != nil {
			return fmt.Errorf("env %s: %w", key, err)
		}
	}
	return nil
}

func setScalar(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("parse bool: %w", err)
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("parse int: %w", err)
		}
		fv.SetInt(n)
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice element %s", fv.Type().Elem().Kind())
		}
		parts := strings.Split(raw, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		fv.Set(reflect.ValueOf(parts))
	default:
		return fmt.Errorf("unsupported kind %s", fv.Kind())
	}
	return nil
}
