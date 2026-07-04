// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的系统配置子域，负责系统配置读写（含敏感字段脱敏）、能力查询、LLM 连通性测试与目录浏览。

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"manga-manager/internal/config"
	"manga-manager/internal/metadata"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func (c *Controller) browseDirs(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")

	// 如果没有传路径，返回系统根目录
	if reqPath == "" {
		if runtime.GOOS == "windows" {
			reqPath = "C:\\"
		} else {
			reqPath = "/"
		}
	}

	// 确保路径存在且是目录
	info, err := os.Stat(reqPath)
	if err != nil || !info.IsDir() {
		jsonError(w, http.StatusBadRequest, "Path is not a valid directory")
		return
	}

	entries, err := os.ReadDir(reqPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Cannot read directory")
		return
	}

	type DirEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	var dirs []DirEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// 跳过隐藏文件夹
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dirs = append(dirs, DirEntry{
			Name: entry.Name(),
			Path: filepath.Join(reqPath, entry.Name()),
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	// Windows 盘符探测
	var drives []DirEntry
	if runtime.GOOS == "windows" {
		for letter := 'A'; letter <= 'Z'; letter++ {
			drivePath := string(letter) + ":\\"
			if fi, err := os.Stat(drivePath); err == nil && fi.IsDir() {
				drives = append(drives, DirEntry{
					Name: string(letter) + ":",
					Path: drivePath,
				})
			}
		}
	}

	result := struct {
		Current string     `json:"current"`
		Parent  string     `json:"parent"`
		Dirs    []DirEntry `json:"dirs"`
		Drives  []DirEntry `json:"drives,omitempty"`
	}{
		Current: reqPath,
		Parent:  filepath.Dir(reqPath),
		Dirs:    dirs,
		Drives:  drives,
	}

	if result.Dirs == nil {
		result.Dirs = []DirEntry{}
	}

	jsonResponse(w, http.StatusOK, result)
}

func (c *Controller) enrichConfigWithDatabase(ctx context.Context, cfg *config.Config) {
	libs, err := c.store.ListLibraries(ctx)
	if err == nil {
		cfg.Library.Paths = make([]string, 0, len(libs))
		for _, lib := range libs {
			cfg.Library.Paths = append(cfg.Library.Paths, lib.Path)
		}
	}
}

func (c *Controller) getSystemConfig(w http.ResponseWriter, r *http.Request) {
	cfg := c.currentConfig()
	c.enrichConfigWithDatabase(r.Context(), &cfg)
	// 回显前脱敏：LLM api_key、server.auth.token 等敏感字段以占位符返回，不向客户端泄露明文。
	jsonResponse(w, http.StatusOK, c.buildSystemConfigResponse(config.MaskSecrets(cfg)))
}

func (c *Controller) getSystemCapabilities(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, c.systemCapabilities())
}

func (c *Controller) updateSystemConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid configuration format")
		return
	}
	// 前端保存整份配置时会把未改动的密钥以占位符形式回传，用当前值回填，避免真实密钥被占位符覆盖。
	config.RestoreMaskedSecrets(&newCfg, c.currentConfig())
	config.NormalizeConfig(&newCfg)

	validation := config.ValidateConfig(&newCfg)
	if !validation.Valid {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "Configuration validation failed",
			"validation": validation,
		})
		return
	}
	if err := c.persistConfig(&newCfg); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to persist configuration")
		return
	}

	c.enrichConfigWithDatabase(r.Context(), &newCfg)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":    apiText(requestLocale(r), "config.saved"),
		"config":     config.MaskSecrets(newCfg),
		"validation": validation,
	})
}

func (c *Controller) testLLMConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider    string `json:"provider"`
		APIMode     string `json:"api_mode"`
		BaseURL     string `json:"base_url"`
		RequestPath string `json:"request_path"`
		Endpoint    string `json:"endpoint"`
		Model       string `json:"model"`
		APIKey      string `json:"api_key"`
		Prompt      string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Prompt == "" {
		req.Prompt = "Hello, this is a test from Manga Manager."
	}
	if req.BaseURL == "" && req.Endpoint != "" {
		tmpCfg := &config.Config{}
		tmpCfg.LLM.Provider = req.Provider
		tmpCfg.LLM.Endpoint = req.Endpoint
		config.NormalizeConfig(tmpCfg)
		req.BaseURL = tmpCfg.LLM.BaseURL
		req.RequestPath = tmpCfg.LLM.RequestPath
		req.APIMode = tmpCfg.LLM.APIMode
	}

	cfg := c.currentConfig()
	// 前端可能回传脱敏占位符（未改动密钥）：用当前存储的真实密钥替换，避免用占位符去测试。
	if req.APIKey == config.SecretMask {
		req.APIKey = cfg.LLM.APIKey
	}
	// SSRF 加固：拒绝非 http(s) 的出站目标协议。
	if err := validateOutboundLLMTarget(req.BaseURL, req.Endpoint); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider := metadata.NewAIProvider(req.Provider, req.APIMode, req.BaseURL, req.RequestPath, req.Model, req.APIKey, cfg.LLM.Timeout)
	response, err := provider.TestLLM(r.Context(), req.Prompt)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("LLM Test failed: %v", err))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"response": response,
	})
}
