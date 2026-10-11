package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewerThan(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"补丁号更大", "0.7.1", "0.7.0", true},
		{"相同版本", "0.7.0", "0.7.0", false},
		{"旧版本", "0.6.5", "0.7.0", false},
		{"两位数不按字符串比", "0.10.0", "0.9.0", true},
		{"段数不同", "0.7", "0.6.5", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := newerThan(c.a, c.b); got != c.want {
				t.Fatalf("newerThan(%q, %q) = %v，期望 %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestReleaseVersion(t *testing.T) {
	for tag, want := range map[string]string{
		"LinkStar-0.7.0": "0.7.0",
		"v0.7.1":         "0.7.1",
		"0.8.0":          "0.8.0",
	} {
		if got := releaseVersion(tag); got != want {
			t.Fatalf("releaseVersion(%q) = %q，期望 %q", tag, got, want)
		}
	}
}

func TestDownload(t *testing.T) {
	newBin := []byte("new linkstar binary")
	sum := sha256.Sum256(newBin)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	// 每个源一种表现：ok 正常给文件、tampered 给改过的、down 回 502、stall 连上了不给数据
	cases := []struct {
		name       string
		sources    []string
		wantErr    string
		wantFile   string
		wantSource string
	}{
		{name: "第一个源就成功", sources: []string{"ok"}, wantFile: string(newBin), wantSource: "/ok/"},
		{name: "镜像挂了换下一个", sources: []string{"down", "ok"}, wantFile: string(newBin), wantSource: "/ok/"},
		{name: "镜像改了文件换下一个", sources: []string{"tampered", "ok"}, wantFile: string(newBin), wantSource: "/ok/"},
		{name: "镜像卡住不给数据换下一个", sources: []string{"stall", "ok"}, wantFile: string(newBin), wantSource: "/ok/"},
		{name: "全都失败不换", sources: []string{"down", "tampered"}, wantErr: "下载失败", wantFile: "old"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			release := make(chan struct{})
			var lastOK string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mode := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
				// 镜像的用法是把完整的 GitHub 地址拼在前缀后面
				if !strings.HasSuffix(r.URL.Path, "/https://github.com/x/linkstar") {
					t.Errorf("镜像请求路径不对：%s", r.URL.Path)
				}
				switch mode {
				case "ok":
					lastOK = "/ok/"
					_, _ = w.Write(newBin)
				case "tampered":
					_, _ = w.Write([]byte("tampered linkstar bin"))
				case "down":
					w.WriteHeader(http.StatusBadGateway)
				case "stall":
					w.(http.Flusher).Flush()
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}
			}))
			defer srv.Close()
			defer close(release)

			exe := filepath.Join(t.TempDir(), "linkstar")
			if err := os.WriteFile(exe, []byte("old"), 0755); err != nil {
				t.Fatal(err)
			}
			origExe, origStall := executablePath, stallTimeout
			executablePath = func() (string, error) { return exe, nil }
			var sources []string
			for _, m := range c.sources {
				sources = append(sources, srv.URL+"/"+m+"/")
			}
			stallTimeout = 200 * time.Millisecond
			defer func() { executablePath, stallTimeout = origExe, origStall }()

			asset := releaseAsset{Name: "linkstar", Size: int64(len(newBin)), Digest: digest, DownloadURL: "https://github.com/x/linkstar"}
			err := (&UpdateRuntime{}).download(asset, sources)
			if c.wantErr == "" && err != nil {
				t.Fatalf("不该出错：%v", err)
			}
			if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("期望错误含 %q，实际 %v", c.wantErr, err)
			}
			if c.wantSource != "" && lastOK != c.wantSource {
				t.Fatalf("应该从 %s 下成功", c.wantSource)
			}
			got, _ := os.ReadFile(exe)
			if string(got) != c.wantFile {
				t.Fatalf("程序文件内容 = %q，期望 %q", got, c.wantFile)
			}
			if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
				t.Fatal("临时文件没清掉")
			}
		})
	}
}

func TestFetchLatestFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/limited/") {
			w.WriteHeader(http.StatusForbidden) // 直连被 GitHub 限流
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"LinkStar-9.9.9"}`))
	}))
	defer srv.Close()

	origAPI := latestAPI
	latestAPI = "https://api.github.com/repos/x/releases/latest"
	defer func() { latestAPI = origAPI }()

	rel, err := fetchLatest(context.Background(), []string{srv.URL + "/limited/", srv.URL + "/mirror/"})
	if err != nil {
		t.Fatalf("第一个源被限流应换下一个：%v", err)
	}
	if releaseVersion(rel.TagName) != "9.9.9" {
		t.Fatalf("版本 = %s", rel.TagName)
	}
}

func TestCheckBinary(t *testing.T) {
	// 拿测试程序自己当样本：它也是 Go 编的，但模块路径是 linkstar 的测试包
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(t.TempDir(), "junk")
	if err := os.WriteFile(junk, []byte("not a binary"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"随便一个文件不认", junk, "不是 LinkStar"},
		{"go test 编出来的不是 linkstar 主程序", self, "不是 LinkStar"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkBinary(c.path)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("期望错误含 %q，实际 %v", c.wantErr, err)
			}
		})
	}
}

func TestConfigSources(t *testing.T) {
	cases := []struct {
		name         string
		cfg          Config
		wantAPI      []string
		wantDownload []string
	}{
		{"老配置没这一段按内置", Config{}, builtinAPI, builtinDownload},
		{"官方只直连", Config{Source: SourceOfficial}, []string{""}, []string{""}},
		{"自定义补上结尾斜杠", Config{Source: SourceCustom, Custom: "https://ghproxy.example.com"},
			[]string{"https://ghproxy.example.com/", ""}, []string{"https://ghproxy.example.com/"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, dl := c.cfg.sources()
			if strings.Join(api, ",") != strings.Join(c.wantAPI, ",") || strings.Join(dl, ",") != strings.Join(c.wantDownload, ",") {
				t.Fatalf("sources() = %v %v，期望 %v %v", api, dl, c.wantAPI, c.wantDownload)
			}
		})
	}
}
