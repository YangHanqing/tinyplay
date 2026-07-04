package i18n

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

const (
	ZH = "zh-CN"
	EN = "en"
)

var messages = map[string]map[string]string{
	ZH: {
		"default_server_name":      "Emby 服务器",
		"invalid_body":             "请求体无效",
		"server_not_found":         "找不到该服务器",
		"host_index_out_of_range":  "host_index 超出范围",
		"no_emby_server":           "没有可用的 Emby 服务器，请先添加并登录",
		"no_server_address":        "还没有配置服务器地址",
		"no_emby_config":           "还没有配置 Emby 服务器",
		"emby_request_failed":      "请求 Emby 失败",
		"auth_failed":              "认证失败，请重新登录",
		"endpoint_not_found":       "接口不存在：%s",
		"emby_http":                "Emby 返回 HTTP %d",
		"bad_credentials":          "账号或密码不正确",
		"login_endpoint_not_found": "没有找到 Emby 登录接口",
		"missing_login_token":      "登录成功但没有返回 AccessToken/User.Id",
		"login_failed":             "登录失败：%s",
		"login_failed_short":       "登录失败",
		"frontend_not_built":       "前端未构建（请运行 `make sync`）",
		"desktop_intro":            "用手机扫描下方二维码，即可在浏览器中遥控本机的 mpv 播放器。请确保手机与本机处于同一局域网。",
		"mpv_unavailable":          "未找到 mpv，请恢复完整应用或安装 mpv",
		"mpv_source_custom":        "高级覆盖",
		"mpv_source_bundled":       "内置",
		"mpv_source_system":        "系统",
		"weekday_monday":           "周一",
		"weekday_tuesday":          "周二",
		"weekday_wednesday":        "周三",
		"weekday_thursday":         "周四",
		"weekday_friday":           "周五",
		"weekday_saturday":         "周六",
		"weekday_sunday":           "周日",
		"open_main":                "打开主界面",
		"open_main_tip":            "显示二维码，扫码遥控",
		"open_logs":                "打开日志目录",
		"open_logs_tip":            "出问题时把这里的日志发给作者",
		"quit":                     "退出",
		"quit_tip":                 "停止服务并退出",
		"tooltip":                  "TV Remote MPV - 手机遥控 mpv",
		"log_start":                "TV Remote MPV 启动；日志目录: %s",
		"log_ready":                "TV Remote MPV 已启动，手机访问： %s",
		"log_http_failed":          "HTTP 服务启动失败: %v",
		"log_bind_failed":          "无法绑定任何端口: %v",
		"log_player_state":         "查询播放状态",
		"log_play":                 "▶ 播放",
		"log_player_command":       "▶ 播放器指令",
		"log_stop":                 "■ 停止",
		"log_props":                "查询播放属性",
		"log_libraries":            "获取媒体库",
		"log_resume":               "获取最近观看",
		"log_items":                "获取媒体列表",
		"log_episodes":             "获取剧集列表",
		"log_servers_list":         "查询服务器列表",
		"log_server_add":           "添加服务器",
		"log_server_activate":      "切换服务器",
		"log_server_host":          "切换 IP",
		"log_server_login":         "登录服务器",
		"log_server_edit":          "编辑服务器",
		"log_server_delete":        "删除服务器",
		"log_server_get":           "查询服务器",
		"log_settings":             "设置",
	},
	EN: {
		"default_server_name":      "Emby Server",
		"invalid_body":             "Invalid request body",
		"server_not_found":         "Server not found",
		"host_index_out_of_range":  "host_index is out of range",
		"no_emby_server":           "No Emby server is available. Add and sign in first.",
		"no_server_address":        "No server address is configured",
		"no_emby_config":           "No Emby server is configured",
		"emby_request_failed":      "Emby request failed",
		"auth_failed":              "Authentication failed. Please sign in again.",
		"endpoint_not_found":       "Endpoint not found: %s",
		"emby_http":                "Emby returned HTTP %d",
		"bad_credentials":          "Incorrect username or password",
		"login_endpoint_not_found": "No Emby login endpoint found",
		"missing_login_token":      "Login succeeded but AccessToken/User.Id was missing",
		"login_failed":             "Login failed: %s",
		"login_failed_short":       "Login failed",
		"frontend_not_built":       "frontend not built (run `make sync`)",
		"desktop_intro":            "Scan the QR code with your phone to control mpv in the browser. Make sure your phone and this computer are on the same local network.",
		"mpv_unavailable":          "mpv unavailable — restore the app or install mpv",
		"mpv_source_custom":        "custom",
		"mpv_source_bundled":       "bundled",
		"mpv_source_system":        "system",
		"weekday_monday":           "Monday",
		"weekday_tuesday":          "Tuesday",
		"weekday_wednesday":        "Wednesday",
		"weekday_thursday":         "Thursday",
		"weekday_friday":           "Friday",
		"weekday_saturday":         "Saturday",
		"weekday_sunday":           "Sunday",
		"open_main":                "Open Remote",
		"open_main_tip":            "Show QR code for phone remote",
		"open_logs":                "Open Logs",
		"open_logs_tip":            "Send these logs when reporting an issue",
		"quit":                     "Quit",
		"quit_tip":                 "Stop the service and quit",
		"tooltip":                  "TV Remote MPV - phone remote for mpv",
		"log_start":                "TV Remote MPV starting; log directory: %s",
		"log_ready":                "TV Remote MPV ready, phone URL: %s",
		"log_http_failed":          "HTTP service failed: %v",
		"log_bind_failed":          "Could not bind any port: %v",
		"log_player_state":         "Get player state",
		"log_play":                 "▶ Play",
		"log_player_command":       "▶ Player command",
		"log_stop":                 "■ Stop",
		"log_props":                "Get player props",
		"log_libraries":            "Get libraries",
		"log_resume":               "Get recent items",
		"log_items":                "Get media items",
		"log_episodes":             "Get episodes",
		"log_servers_list":         "List servers",
		"log_server_add":           "Add server",
		"log_server_activate":      "Switch server",
		"log_server_host":          "Switch IP",
		"log_server_login":         "Sign in server",
		"log_server_edit":          "Edit server",
		"log_server_delete":        "Delete server",
		"log_server_get":           "Get server",
		"log_settings":             "Settings",
	},
}

func Normalize(value string) string {
	if strings.HasPrefix(strings.ToLower(value), "zh") {
		return ZH
	}
	return EN
}

func SystemLang() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(key); value != "" {
			return Normalize(value)
		}
	}
	return EN
}

func RequestLang(r *http.Request) string {
	if r == nil {
		return SystemLang()
	}
	header := r.Header.Get("Accept-Language")
	if header == "" {
		return SystemLang()
	}
	for _, part := range strings.Split(header, ",") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(part)), "zh") {
			return ZH
		}
	}
	return EN
}

func T(lang, key string, args ...any) string {
	lang = Normalize(lang)
	template := messages[lang][key]
	if template == "" {
		template = messages[EN][key]
	}
	if template == "" {
		template = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(template, args...)
	}
	return template
}

func System(key string, args ...any) string {
	return T(SystemLang(), key, args...)
}

func Request(r *http.Request, key string, args ...any) string {
	return T(RequestLang(r), key, args...)
}

func LocalizeError(lang, msg string) string {
	pairs := map[string]string{
		"No Emby server is available. Add and sign in first.": "no_emby_server",
		"No server address is configured":                     "no_server_address",
		"No Emby server is configured":                        "no_emby_config",
		"Emby request failed":                                 "emby_request_failed",
		"Authentication failed. Please sign in again.":        "auth_failed",
		"Incorrect username or password":                      "bad_credentials",
		"No Emby login endpoint found":                        "login_endpoint_not_found",
		"Login succeeded but AccessToken/User.Id was missing": "missing_login_token",
		"Login failed":     "login_failed_short",
		"Server not found": "server_not_found",
		"没有可用的 Emby 服务器，请先添加并登录":        "no_emby_server",
		"还没有配置服务器地址":                    "no_server_address",
		"还没有配置 Emby 服务器":                "no_emby_config",
		"请求 Emby 失败":                    "emby_request_failed",
		"认证失败，请重新登录":                    "auth_failed",
		"账号或密码不正确":                      "bad_credentials",
		"没有找到 Emby 登录接口":                "login_endpoint_not_found",
		"登录成功但没有返回 AccessToken/User.Id": "missing_login_token",
		"登录失败":                          "login_failed_short",
		"找不到该服务器":                       "server_not_found",
	}
	if key, ok := pairs[msg]; ok {
		return T(lang, key)
	}
	for prefix, key := range map[string]string{
		"Endpoint not found: ": "endpoint_not_found",
		"Login failed: ":       "login_failed",
		"Emby returned HTTP ":  "emby_http",
		"接口不存在：":               "endpoint_not_found",
		"登录失败：":                "login_failed",
		"Emby 返回 HTTP ":        "emby_http",
	} {
		if rest, ok := strings.CutPrefix(msg, prefix); ok {
			if key == "emby_http" {
				var code int
				_, _ = fmt.Sscanf(rest, "%d", &code)
				return T(lang, key, code)
			}
			return T(lang, key, rest)
		}
	}
	return msg
}
