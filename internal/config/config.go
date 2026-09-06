package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFilePerm 是 config.yaml 的落盘权限：仅属主可读写。
//
// 该文件里存着 llm.api_key 与刮削器凭据（mal_client_id / comicvine_api_key）的明文。
// 用 0644 落盘等于把这些密钥交给本机任意用户，而这恰好架空了 MaskSecrets 那套
// 「不向客户端回显明文密钥」的加固——前端拿不到，本机 shell 一个 cat 就拿到了。
//
// 注意这是**确定性**的收紧而非依赖 umask：os.CreateTemp 本就建出 0600，
// AtomicWriteFile 随后的 os.Chmod 不经 umask 过滤，rename 又是整个 inode 替换，
// 所以最终权限精确等于这里传的值，中间也不存在短暂全局可读的窗口。
//
// 存量安装不会被自动收紧（createDefaultConfig 只在文件不存在时触发），
// 需要用户在设置页保存一次，或按 README 手动 chmod 600。
const ConfigFilePerm os.FileMode = 0o600

// ConfigDirPerm 是自动补建配置目录时的权限：仅属主可进入。
//
// 目录里装的正是 ConfigFilePerm(0600) 那份含明文密钥的 config.yaml，父目录若对全体可读
// 可进入，本机任意用户至少能枚举到它、也能在其中留下自己的文件。目录已存在时 MkdirAll
// 不改权限，所以这只影响我们新建的那一层，不会去动用户自己摆好的 /etc 之类共享目录。
const ConfigDirPerm os.FileMode = 0o700

// AtomicWriteFile 原子写文件：先写入同目录临时文件再 rename 覆盖目标，避免写入过程中崩溃/断电
// 留下半截配置文件（半截 YAML 会让下次启动解析失败且无自愈路径）。os.Rename 在 Windows 上也会
// 原子替换已存在文件。
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后临时文件已不存在，这里的 remove 无害
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

type Config struct {
	Server struct {
		Host           string   `yaml:"host" json:"host"`
		Port           int      `yaml:"port" json:"port"`
		AllowedOrigins []string `yaml:"allowed_origins" json:"allowed_origins"`
		// TrustedProxies 是可信反向代理的 CIDR 列表（如 ["127.0.0.1/32", "10.0.0.0/8"]）。
		// 只有当直连对端落在这些网段内时，才采信 X-Forwarded-For / X-Real-IP 里的客户端 IP。
		//
		// 默认为空 = 一律不信任转发头。这一点很关键：限流（登录暴破、OPDS/Mihon 的 bcrypt
		// CPU-DoS 防护）都以客户端 IP 为键，无条件采信 XFF 意味着攻击者每个请求换一个伪造
		// IP 就能把限流完全绕过。直连部署本就不该有转发头；套了反代的部署显式配上网段即可。
		TrustedProxies []string `yaml:"trusted_proxies" json:"trusted_proxies"`
		// CookieSecure 决定会话 Cookie 是否带 Secure 标志：auto（默认）/ always / never。
		//
		// auto 与 TrustedProxies 共用同一套「代理头可不可信」口径：直连 TLS，或直连对端落在
		// TrustedProxies 内且它声明 X-Forwarded-Proto: https。所以 TrustedProxies 留空时，
		// 反代后的 HTTPS 部署在 auto 下拿不到 Secure —— 这不是疏漏，是「不信任未声明来源的头」
		// 的必然结果。
		//
		// 留这个字段就是为了不逼管理员在「放宽限流的信任面」和「丢掉 Secure」之间二选一：
		// 这本来是两个独立决定。TLS 在反代终结、又不想把反代网段填进 TrustedProxies 的部署，
		// 显式写 always 即可；反向地，反代错误声明了 https 而实际服务明文时，Secure 会让浏览器
		// 直接丢弃 Cookie（登录彻底失效），此时用 never 压回去。
		CookieSecure string `yaml:"cookie_secure" json:"cookie_secure"`
	} `yaml:"server" json:"server"`
	Database struct {
		Path string `yaml:"path" json:"path"`
	} `yaml:"database" json:"database"`
	Library struct {
		Paths           []string               `yaml:"paths" json:"paths"`
		StorageProfile  string                 `yaml:"storage_profile" json:"storage_profile"`
		IOPolicy        StorageIOPolicy        `yaml:"io_policy" json:"io_policy"`
		StoragePolicies []LibraryStoragePolicy `yaml:"storage_policies" json:"storage_policies"`
	} `yaml:"library" json:"library"`
	Cache struct {
		Dir                  string `yaml:"dir" json:"dir"`
		PageDiskCacheEnabled bool   `yaml:"page_disk_cache_enabled" json:"page_disk_cache_enabled"`
		// PageDiskCacheMaxBytes 是磁盘页缓存的容量上限（字节）。0 归一化为默认值 2GiB，负数表示不限。
		PageDiskCacheMaxBytes int64 `yaml:"page_disk_cache_max_bytes" json:"page_disk_cache_max_bytes"`
	} `yaml:"cache" json:"cache"`
	Logging struct {
		Level string `yaml:"level" json:"level"`
	} `yaml:"logging" json:"logging"`
	Scanner struct {
		Workers          int    `yaml:"workers" json:"workers"`
		ScanProfile      string `yaml:"scan_profile" json:"scan_profile"`
		ThumbnailFormat  string `yaml:"thumbnail_format" json:"thumbnail_format"`
		Waifu2xPath      string `yaml:"waifu2x_path" json:"waifu2x_path"`
		RealCuganPath    string `yaml:"realcugan_path" json:"realcugan_path"`
		ArchivePoolSize  int    `yaml:"archive_pool_size" json:"archive_pool_size"`
		MaxAiConcurrency int    `yaml:"max_ai_concurrency" json:"max_ai_concurrency"`
	} `yaml:"scanner" json:"scanner"`
	// Tasks 是后台**运行**这一侧的设置。
	Tasks struct {
		// RunSlots 是**运行槽位**上限：全局同时运行数最多几条，也是任务中心那格「槽位 n/N」
		// 的分母。超出的运行留在**排队中**等放行。0（未设置）归一化为默认值。
		//
		// 名字跟着词汇表走。不叫「并发上限」是因为那个说法在本仓已经有主：`run_limits`
		// 那张侧表记的是一次运行实际生效的那 11 项并发上限（扫描并发、封面并发……），
		// 与「同时能跑几条运行」是两回事。
		//
		// 它是**单一全局数字**而不是按卷键限（ADR 0005 的知情取舍）：按卷更贴近真实瓶颈——
		// 两个库在两块盘上本可以并行——但一个数字更好解释、界面上也画得出。代价是多盘用户
		// 会看到一条运行在排队，而它要等的那块盘是空的。
		RunSlots int `yaml:"run_slots" json:"run_slots"`

		// RetainRunsPerTask 是每个任务保留的最近**终态**运行条数，也是**分层保留**三个阈值的头一个。
		//
		// 三个阈值都会**删数据**：调小之后下一次清理就把落在阈值之外的历史删掉，删掉回不来。
		// 小于 1 的值一律归一化为默认值——「一条都不留」不是任何人想要的意思，而 0 是「配置文件里
		// 没写」。要留得久就把数字调大，本仓不为「永久保留」另设一个哨兵值。
		// **活动态与排队中的运行不受这三个数影响**，那不是策略而是前提（见 task.RetentionPolicy）。
		RetainRunsPerTask int `yaml:"retain_runs_per_task" json:"retain_runs_per_task"`
		// RetainTerminalRunDays 是终态运行的最长保留天数，与 RetainRunsPerTask 取先到者。
		RetainTerminalRunDays int `yaml:"retain_terminal_run_days" json:"retain_terminal_run_days"`
		// RetainSampleDays 是**采样**点的最长保留天数。它比运行本身短：运行还在，曲线没了。
		RetainSampleDays int `yaml:"retain_sample_days" json:"retain_sample_days"`

		// **退避**的三个阈值。它们只约束**自动发起**（定时与监听）：手动发起、以及已经在跑的
		// 运行都不受影响。小于 1 的值一律归一化为默认值，逐项失效的方式见 task.BackoffPolicy.orDefault。

		// BackoffFactor 是倍率：每多连败一次，自动发起的间隔乘以它。
		BackoffFactor int `yaml:"backoff_factor" json:"backoff_factor"`
		// BackoffMaxHours 是封顶：间隔再怎么翻也不超过这么多小时。
		BackoffMaxHours int `yaml:"backoff_max_hours" json:"backoff_max_hours"`
		// BackoffStopAfter 是停发阈值：连败到这个次数就不再自动发起，界面上标红等人来修。
		BackoffStopAfter int `yaml:"backoff_stop_after" json:"backoff_stop_after"`

		// ResumeAfterRestart 是**可续跑**的全局开关：服务重启后，白名单内的工作自己接着跑，
		// **发起方**记恢复。**默认开**——它防的是「无人值守的机器重启后要有人登录去点重试」。
		//
		// 关掉之后仍会变化的运行照样全部转**中断**，只是一条都不自动重排队。
		// 它只是开关：哪些类型进白名单不可配（判据是「会不会改磁盘内容、会不会花钱」，
		// 见 task.ResumePolicy），把它做成一份可配清单等于把那条判据交给配置文件。
		//
		// 指针是为了分开「配置文件里没写」（nil，归一化成开着）与「明确关掉」（false）——
		// 一个默认为真的布尔值用零值表达不出前者。新增指针字段必须同时补 CloneConfig。
		ResumeAfterRestart *bool `yaml:"resume_after_restart" json:"resume_after_restart"`
	} `yaml:"tasks" json:"tasks"`
	Ollama struct {
		Endpoint string `yaml:"endpoint" json:"endpoint"`
		Model    string `yaml:"model" json:"model"`
	} `yaml:"ollama" json:"ollama"` // Deprecated: Use LLM instead

	LLM struct {
		Provider    string `yaml:"provider" json:"provider"`         // e.g. "ollama", "openai"
		APIMode     string `yaml:"api_mode" json:"api_mode"`         // "responses" or "chat_completions"
		BaseURL     string `yaml:"base_url" json:"base_url"`         // e.g. "http://localhost:11434" or "https://api.openai.com"
		RequestPath string `yaml:"request_path" json:"request_path"` // e.g. "/v1/responses"
		Endpoint    string `yaml:"endpoint" json:"endpoint"`         // Deprecated: kept for backwards compatibility
		Model       string `yaml:"model" json:"model"`               // e.g. "qwen2.5" or "gpt-4o"
		APIKey      string `yaml:"api_key" json:"api_key"`           // Optional API Key for OpenAI/DeepSeek
		Timeout     int    `yaml:"timeout" json:"timeout"`           // 请求超时时间（秒），默认 120
	} `yaml:"llm" json:"llm"`
	// Scrapers 存放需要凭据的外部元数据源密钥。AniList / MangaDex 免密钥，无需在此配置；
	// MyAnimeList 需要 Client ID，Comic Vine 需要 API Key，未填则对应源在 listProviders 中不出现。
	Scrapers struct {
		MALClientID     string `yaml:"mal_client_id" json:"mal_client_id"`
		ComicVineAPIKey string `yaml:"comicvine_api_key" json:"comicvine_api_key"`
	} `yaml:"scrapers" json:"scrapers"`
	Protocols struct {
		OPDS struct {
			Enabled bool `yaml:"enabled" json:"enabled"`
		} `yaml:"opds" json:"opds"`
		Mihon struct {
			Enabled bool `yaml:"enabled" json:"enabled"`
		} `yaml:"mihon" json:"mihon"`
	} `yaml:"protocols" json:"protocols"`
	KOReader struct {
		Enabled             bool   `yaml:"enabled" json:"enabled"`
		BasePath            string `yaml:"base_path" json:"base_path"`
		AllowRegistration   bool   `yaml:"allow_registration" json:"allow_registration"`
		MatchMode           string `yaml:"match_mode" json:"match_mode"`
		PathIgnoreExtension bool   `yaml:"path_ignore_extension" json:"path_ignore_extension"`
	} `yaml:"koreader" json:"koreader"`
}

const (
	KOReaderMatchModeBinaryHash = "binary_hash"
	KOReaderMatchModeFilePath   = "file_path"
	// DefaultPageDiskCacheMaxBytes 磁盘页缓存默认容量上限（2 GiB）。
	DefaultPageDiskCacheMaxBytes = 2 << 30
	// DefaultRunSlots 是**运行槽位**的默认上限。
	//
	// 它必须与 `internal/task` 的 DefaultSlots 相等：那一个是「装配方什么都没说」时的兜底，
	// 这一个是「配置文件里没写」时的默认，两者错开会让默认部署与默认引擎给出两个不同的分母。
	// 本包不能引用它——config 位于 task 的依赖下游，反向引用会成环。
	// `TestConfigDefaultSlotsMatchTheEngineDefault` 守着这条相等。
	DefaultRunSlots = 2

	// DefaultRetain* 是**分层保留**三个阈值的默认值：每任务留最近 20 次终态运行、
	// 终态运行 ≤90 天（两者取先到者）、**采样** ≤7 天。
	//
	// 它们必须与 `internal/task` 的 DefaultRetention 相等，理由同 DefaultRunSlots：
	// 那一个是「装配方什么都没说」时的兜底，这一个是「配置文件里没写」时的默认。
	// `TestConfigDefaultRetentionMatchesTheEngineDefault` 守着这条相等。
	DefaultRetainRunsPerTask     = 20
	DefaultRetainTerminalRunDays = 90
	DefaultRetainSampleDays      = 7

	// DefaultBackoff* 是**退避**三个阈值的默认值：连败后按 ×2 拉长自动发起的间隔、
	// 封顶 24 小时、连败 6 次停发。
	//
	// 它们必须与 `internal/task` 的 DefaultBackoff 相等，理由同 DefaultRunSlots。
	// `TestConfigDefaultBackoffMatchesTheEngineDefault` 守着这条相等。
	DefaultBackoffFactor    = 2
	DefaultBackoffMaxHours  = 24
	DefaultBackoffStopAfter = 6

	// DefaultResumeAfterRestart 是**可续跑**全局开关的默认值：开着。
	// 配置文件里没写这一项就是它，见 Config.Tasks.ResumeAfterRestart。
	DefaultResumeAfterRestart = true

	KOReaderPathMatchDepth = 2
	LogLevelDebug          = "debug"
	LogLevelInfo           = "info"
	LogLevelWarn           = "warn"
	LogLevelError          = "error"

	// CookieSecure* 是 server.cookie_secure 的取值：会话 Cookie 的 Secure 标志由谁说了算。
	//   auto   —— 按连接实际情况判定（直连 TLS，或**可信**代理声明的 X-Forwarded-Proto: https）
	//   always —— 无条件带 Secure，给「TLS 在反代终结、又不想放宽 trusted_proxies」的部署一个出口
	//   never  —— 无条件不带，反向逃生口：反代错误声明了 https 而实际是明文时，
	//             auto 会让浏览器直接丢弃 Cookie（登录彻底失效），需要能手动压回去
	CookieSecureAuto   = "auto"
	CookieSecureAlways = "always"
	CookieSecureNever  = "never"
)

// SecretMask 是回显给前端的敏感字段占位符。前端把它原样存进只写输入框（如 <input type=password>），
// 保存时若字段仍等于该占位符，后端据此保留原值（见 RestoreMaskedSecrets），从而既不向客户端泄露
// 明文密钥，又不会因前端回传占位符而把真实密钥覆盖掉。占位符本身不是任何合法密钥。
const SecretMask = "__mm_secret_unchanged__"

// MaskSecrets 返回 cfg 的副本，将敏感字段（LLM APIKey、刮削器凭据）替换为占位符。
// 仅当字段非空时替换，空值保持为空，便于前端区分“已设置”与“未设置”。
func MaskSecrets(cfg Config) Config {
	if cfg.LLM.APIKey != "" {
		cfg.LLM.APIKey = SecretMask
	}
	if cfg.Scrapers.ComicVineAPIKey != "" {
		cfg.Scrapers.ComicVineAPIKey = SecretMask
	}
	if cfg.Scrapers.MALClientID != "" {
		cfg.Scrapers.MALClientID = SecretMask
	}
	return cfg
}

// RestoreMaskedSecrets 把 incoming 中仍为占位符的敏感字段用 current 的真实值回填。
// 前端保存整份配置时会把未改动的密钥以占位符形式回传，此处据此避免真实密钥被占位符覆盖。
func RestoreMaskedSecrets(incoming *Config, current Config) {
	if incoming == nil {
		return
	}
	if incoming.LLM.APIKey == SecretMask {
		incoming.LLM.APIKey = current.LLM.APIKey
	}
	if incoming.Scrapers.ComicVineAPIKey == SecretMask {
		incoming.Scrapers.ComicVineAPIKey = current.Scrapers.ComicVineAPIKey
	}
	if incoming.Scrapers.MALClientID == SecretMask {
		incoming.Scrapers.MALClientID = current.Scrapers.MALClientID
	}
}

// LoadConfig 读取配置；文件不存在时**生成一份默认配置并写回磁盘**。
//
// 这个写盘副作用只适合启动路径。热重载必须用 LoadConfigFile：编辑器保存常常是
// 「先把 config.yaml rename 走、再写新文件」，事件恰好落在文件缺失的那个窗口时，
// LoadConfig 会把用户的配置**覆盖成默认值**，同时把内存配置也清成默认——
// 一次编辑保存换来整份配置丢失。
func LoadConfig(path string) (*Config, error) {
	cfg, err := LoadConfigFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return createDefaultConfig(path)
		}
		return nil, err
	}
	return cfg, nil
}

// LoadConfigFile 读取并归一化配置，**不做任何写盘**。文件不存在时原样返回 os.ErrNotExist。
// 供热重载等「不该有副作用」的路径使用。
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Backwards compatibility layer
	if cfg.LLM.Provider == "" && cfg.Ollama.Endpoint != "" {
		cfg.LLM.Provider = "ollama"
		cfg.LLM.BaseURL = cfg.Ollama.Endpoint
		cfg.LLM.Model = cfg.Ollama.Model
	}
	// Defaults if LLM is entirely absent
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "ollama"
		cfg.LLM.BaseURL = "http://localhost:11434"
		cfg.LLM.Model = "qwen2.5"
	}
	NormalizeConfig(&cfg)

	return &cfg, nil
}

func createDefaultConfig(path string) (*Config, error) {
	cfg := &Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8080
	cfg.Server.AllowedOrigins = []string{"http://*", "https://*"}
	cfg.Server.CookieSecure = CookieSecureAuto
	cfg.Database.Path = "./data/manga.db"
	cfg.Library.Paths = []string{}
	cfg.Library.StorageProfile = StorageProfileAuto
	cfg.Cache.Dir = "./data/cache"
	cfg.Cache.PageDiskCacheEnabled = false
	cfg.Cache.PageDiskCacheMaxBytes = DefaultPageDiskCacheMaxBytes
	cfg.Logging.Level = LogLevelInfo
	cfg.Scanner.Workers = 0 // 0 表示自动使用 runtime.NumCPU() * 2
	cfg.Scanner.ScanProfile = ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp" // 支持 webp, jpg, avif
	cfg.Scanner.Waifu2xPath = ""
	cfg.Scanner.RealCuganPath = ""
	cfg.Scanner.ArchivePoolSize = 5  // 默认缓存 5 个打开的归档压缩包句柄
	cfg.Scanner.MaxAiConcurrency = 3 // 默认限制最多抛出 3 个外置 AI 渲染子进程

	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.RequestPath = ""
	cfg.LLM.APIMode = ""
	cfg.LLM.Model = "qwen2.5"
	cfg.LLM.Timeout = 120
	cfg.Protocols.OPDS.Enabled = false
	cfg.Protocols.Mihon.Enabled = false
	cfg.KOReader.Enabled = false
	cfg.KOReader.BasePath = "/koreader"
	cfg.KOReader.AllowRegistration = false
	cfg.KOReader.MatchMode = KOReaderMatchModeBinaryHash
	cfg.KOReader.PathIgnoreExtension = false
	NormalizeConfig(cfg)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	// 建的只有配置文件自己的父目录：-config 可以指向容器卷子路径或 /etc/manga/config.yaml 这类
	// 尚不存在的位置，父目录缺失时 AtomicWriteFile 在建同目录临时文件那一步就失败。数据目录不归
	// 这里建——它由 database.path / cache.dir 决定，在 cwd 下写死一个 ./data 只会留下没人用的空目录。
	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, ConfigDirPerm); err != nil {
		return nil, fmt.Errorf("配置目录 %s 不存在且无法创建: %w", configDir, err)
	}

	// 首次生成就按仅属主可读写落盘：此刻密钥还是空的，但用户随后会在设置页填进来，
	// 那时权限位已经定型（persistConfig 用的是同一个常量），没有补救窗口。
	if err := AtomicWriteFile(path, data, ConfigFilePerm); err != nil {
		return nil, fmt.Errorf("写入默认配置 %s 失败: %w", path, err)
	}

	return cfg, nil
}

func normalizeLLMConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" {
		provider = "ollama"
		cfg.LLM.Provider = provider
	}

	if cfg.LLM.BaseURL == "" && cfg.LLM.Endpoint != "" {
		cfg.LLM.BaseURL, cfg.LLM.RequestPath = splitEndpoint(cfg.LLM.Endpoint)
	}

	switch provider {
	case "openai-legacy":
		cfg.LLM.Provider = "openai"
		if cfg.LLM.APIMode == "" {
			cfg.LLM.APIMode = "chat_completions"
		}
	case "openai":
		if cfg.LLM.APIMode == "" {
			cfg.LLM.APIMode = inferAPIModeFromRequestPath(cfg.LLM.RequestPath)
		}
	default:
		cfg.LLM.APIMode = ""
		cfg.LLM.RequestPath = ""
	}

	if cfg.LLM.BaseURL == "" {
		if cfg.LLM.Provider == "openai" {
			cfg.LLM.BaseURL = "https://api.openai.com"
		} else {
			cfg.LLM.BaseURL = "http://localhost:11434"
		}
	}

	if cfg.LLM.Provider == "openai" && cfg.LLM.RequestPath == "" {
		cfg.LLM.RequestPath = defaultRequestPath(cfg.LLM.APIMode)
	}

	cfg.LLM.Endpoint = BuildLLMEndpoint(cfg)

	if cfg.LLM.Timeout <= 0 {
		cfg.LLM.Timeout = 120
	}
}

func NormalizeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	cfg.Server.Host = strings.TrimSpace(cfg.Server.Host)
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	cfg.Server.AllowedOrigins = normalizeAllowedOrigins(cfg.Server.AllowedOrigins)
	cfg.Server.CookieSecure = normalizeCookieSecure(cfg.Server.CookieSecure)
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/manga.db"
	}
	NormalizeLibraryStorageConfig(cfg)
	if cfg.Cache.Dir == "" {
		cfg.Cache.Dir = "./data/cache"
	}
	// 0（未设置）归一化为默认上限，使既有配置也获得容量保护；负数表示显式不限。
	if cfg.Cache.PageDiskCacheMaxBytes == 0 {
		cfg.Cache.PageDiskCacheMaxBytes = DefaultPageDiskCacheMaxBytes
	}
	level := strings.ToLower(strings.TrimSpace(cfg.Logging.Level))
	switch level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		level = LogLevelInfo
	}
	cfg.Logging.Level = level
	if cfg.Scanner.ThumbnailFormat == "" {
		cfg.Scanner.ThumbnailFormat = "webp"
	}
	cfg.Scanner.ScanProfile = NormalizeScanProfile(cfg.Scanner.ScanProfile)
	if cfg.Scanner.ArchivePoolSize == 0 {
		cfg.Scanner.ArchivePoolSize = 5
	}
	if cfg.Scanner.MaxAiConcurrency == 0 {
		cfg.Scanner.MaxAiConcurrency = 3
	}
	// 小于 1 的上限不是「不限」而是「一条都不许跑」，照它办事等于把整台机器的后台工作卡死。
	if cfg.Tasks.RunSlots < 1 {
		cfg.Tasks.RunSlots = DefaultRunSlots
	}
	// 保留阈值同理：小于 1 是「一条都不留 / 一天都不留」，照它办事等于把历史当场删光。
	if cfg.Tasks.RetainRunsPerTask < 1 {
		cfg.Tasks.RetainRunsPerTask = DefaultRetainRunsPerTask
	}
	if cfg.Tasks.RetainTerminalRunDays < 1 {
		cfg.Tasks.RetainTerminalRunDays = DefaultRetainTerminalRunDays
	}
	if cfg.Tasks.RetainSampleDays < 1 {
		cfg.Tasks.RetainSampleDays = DefaultRetainSampleDays
	}
	// 退避的三个数同理。「不要退避」的表达是把停发阈值调大、把封顶调小，不是把某个数填成 0。
	if cfg.Tasks.BackoffFactor < 1 {
		cfg.Tasks.BackoffFactor = DefaultBackoffFactor
	}
	if cfg.Tasks.BackoffMaxHours < 1 {
		cfg.Tasks.BackoffMaxHours = DefaultBackoffMaxHours
	}
	if cfg.Tasks.BackoffStopAfter < 1 {
		cfg.Tasks.BackoffStopAfter = DefaultBackoffStopAfter
	}
	// 没写就是开着：无人值守的机器重启后不该等人登录去点重试（规格用户故事 19）。
	if cfg.Tasks.ResumeAfterRestart == nil {
		resume := DefaultResumeAfterRestart
		cfg.Tasks.ResumeAfterRestart = &resume
	}
	normalizeLLMConfig(cfg)
	basePath := strings.TrimSpace(cfg.KOReader.BasePath)
	if basePath == "" {
		basePath = "/koreader"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	if basePath == "//" || basePath == "" {
		basePath = "/koreader"
	}
	cfg.KOReader.BasePath = basePath
	matchMode := strings.TrimSpace(strings.ToLower(cfg.KOReader.MatchMode))
	switch matchMode {
	case KOReaderMatchModeBinaryHash, KOReaderMatchModeFilePath:
	default:
		matchMode = KOReaderMatchModeBinaryHash
	}
	cfg.KOReader.MatchMode = matchMode
}

// normalizeCookieSecure 把 server.cookie_secure 收敛到三个合法取值之一。
//
// 非法值回落 auto 并告警，而不是留给 ValidateConfig：LoadConfig 只调 NormalizeConfig，
// 启动路径上根本没人跑 ValidateConfig（它只服务于设置页的保存前校验）。若这里不处理，
// 把 "alwyas" 写错的管理员得到的就是一次完全无声的降级——正是这次要消灭的那类问题。
func normalizeCookieSecure(raw string) string {
	switch value := strings.ToLower(strings.TrimSpace(raw)); value {
	case CookieSecureAuto, CookieSecureAlways, CookieSecureNever:
		return value
	case "":
		return CookieSecureAuto
	default:
		slog.Warn("Unknown server.cookie_secure value, falling back to auto",
			"value", raw, "allowed", []string{CookieSecureAuto, CookieSecureAlways, CookieSecureNever})
		return CookieSecureAuto
	}
}

func normalizeAllowedOrigins(origins []string) []string {
	normalized := make([]string, 0, len(origins))
	seen := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		normalized = append(normalized, origin)
	}
	if len(normalized) == 0 {
		return []string{"http://*", "https://*"}
	}
	return normalized
}

func splitEndpoint(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw, ""
	}

	base := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	requestPath := parsed.EscapedPath()
	if parsed.RawQuery != "" {
		requestPath += "?" + parsed.RawQuery
	}
	return base, requestPath
}

func inferAPIModeFromRequestPath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.Contains(path, "chat/completions"):
		return "chat_completions"
	case strings.Contains(path, "responses"):
		return "responses"
	default:
		return "responses"
	}
}

func defaultRequestPath(apiMode string) string {
	if strings.EqualFold(apiMode, "chat_completions") {
		return "/v1/chat/completions"
	}
	return "/v1/responses"
}

func BuildLLMEndpoint(cfg *Config) string {
	if cfg == nil {
		return ""
	}

	baseURL := strings.TrimSpace(cfg.LLM.BaseURL)
	if baseURL == "" {
		return ""
	}
	if cfg.LLM.Provider != "openai" {
		return strings.TrimRight(baseURL, "/")
	}

	requestPath := strings.TrimSpace(cfg.LLM.RequestPath)
	if requestPath == "" {
		requestPath = defaultRequestPath(cfg.LLM.APIMode)
	}

	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(requestPath, "/")
}
