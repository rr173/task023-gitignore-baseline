// Package selfcheck 提供无需外部依赖的自检：通过 httptest 启动真实 HTTP
// 服务，覆盖判定端点与各边界约束。成功返回 0，任一失败返回 1。
package selfcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"task023-gitignore/internal/gitignore"
	"task023-gitignore/internal/httpapi"
)

// Run 执行自检并返回退出码。
func Run() int {
	passed, failed := 0, 0
	check := func(name string, fn func() error) {
		if err := fn(); err != nil {
			failed++
			fmt.Printf("FAIL %-36s %v\n", name, err)
		} else {
			passed++
			fmt.Printf("PASS %s\n", name)
		}
	}

	srv := httptest.NewServer(httpapi.New().Handler())
	defer srv.Close()

	do := func(method, path, body string) (*http.Response, []byte, error) {
		var r io.Reader
		if body != "" {
			r = bytes.NewReader([]byte(body))
		}
		req, err := http.NewRequest(method, srv.URL+path, r)
		if err != nil {
			return nil, nil, err
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, data, readErr
	}
	marshal := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return string(b)
	}

	// check 端点封装。
	checkEnd := func(rules string, paths []string) (int, *gitignore.Result, []string, []string, string, error) {
		body := marshal(map[string]any{"rules": rules, "paths": paths})
		resp, data, err := do(http.MethodPost, "/check", body)
		if err != nil {
			return 0, nil, nil, nil, "", err
		}
		var out struct {
			Results []gitignore.Result `json:"results"`
			Ignored []string           `json:"ignored"`
			Kept    []string           `json:"kept"`
			Error   string             `json:"error"`
		}
		_ = json.Unmarshal(data, &out)
		if out.Results == nil {
			out.Results = []gitignore.Result{}
		}
		if out.Ignored == nil {
			out.Ignored = []string{}
		}
		if out.Kept == nil {
			out.Kept = []string{}
		}
		// 返回首个结果（测试通常单路径）。
		var first *gitignore.Result
		if len(out.Results) > 0 {
			r := out.Results[0]
			first = &r
		}
		return resp.StatusCode, first, out.Ignored, out.Kept, out.Error, nil
	}
	// findResult 返回 (是否出现, 是否在保留列表)。
	findResult := func(kept []string, ignored []string, path string) (bool, bool) {
		for _, p := range ignored {
			if p == path {
				return true, false
			}
		}
		for _, p := range kept {
			if p == path {
				return true, true
			}
		}
		return false, false
	}

	// ---- 健康检查 ----
	check("健康检查", func() error {
		resp, _, err := do(http.MethodGet, "/healthz", "")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	// ---- 基础匹配 ----
	check("基础通配匹配", func() error {
		status, first, ignored, kept, errStr, err := checkEnd("*.log\n", []string{"debug.log", "src/main.go"})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		if first == nil || !first.Ignored || first.Rule != "*.log" || first.Line != 1 || first.Negated {
			return fmt.Errorf("first=%+v want ignored debug.log via *.log@1", first)
		}
		_, isKept := findResult(kept, ignored, "src/main.go")
		if !isKept {
			return fmt.Errorf("src/main.go should be kept, ignored=%v kept=%v", ignored, kept)
		}
		return nil
	})

	// ---- 取反重新包含（约束1）----
	check("取反规则重新包含路径", func() error {
		status, first, _, _, errStr, err := checkEnd("*.log\n!keep.log\n", []string{"keep.log"})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		if first == nil || first.Ignored || first.Rule != "!keep.log" || first.Line != 2 || !first.Negated {
			return fmt.Errorf("keep.log should be kept via !keep.log@2 negated, got %+v", first)
		}
		// debug.log 仍被忽略。
		_, _, ignored2, _, _, err := checkEnd("*.log\n!keep.log\n", []string{"debug.log"})
		if err != nil {
			return err
		}
		if len(ignored2) != 1 || ignored2[0] != "debug.log" {
			return fmt.Errorf("debug.log should be ignored via *.log, got ignored=%v", ignored2)
		}
		return nil
	})

	// ---- 最后命中决定：再次忽略（约束1）----
	check("最后命中决定再次忽略", func() error {
		// * 忽略全部；!a 保留 a；a 又把 a 重新忽略。
		status, first, _, _, errStr, err := checkEnd("*\n!a\na\n", []string{"a"})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		if first == nil || !first.Ignored || first.Rule != "a" || first.Line != 3 || first.Negated {
			return fmt.Errorf("a should be ignored via a@3 (last match), got %+v", first)
		}
		return nil
	})

	// ---- * 不跨分隔符（约束2）----
	check("星号不跨分隔符", func() error {
		status, first, _, _, errStr, err := checkEnd("a/*.txt\n", []string{"a/sub/notes.txt"})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		if first == nil || first.Ignored {
			return fmt.Errorf("a/sub/notes.txt should be kept, got %+v", first)
		}
		// a/notes.txt 应被忽略。
		_, f2, _, _, _, err := checkEnd("a/*.txt\n", []string{"a/notes.txt"})
		if err != nil {
			return err
		}
		if f2 == nil || !f2.Ignored || f2.Rule != "a/*.txt" {
			return fmt.Errorf("a/notes.txt should be ignored via a/*.txt, got %+v", f2)
		}
		return nil
	})

	// ---- ** 跨任意层级（约束2）----
	check("双星号跨任意层级", func() error {
		status, first, _, _, errStr, err := checkEnd("a/**/b\n", []string{"a/x/y/b"})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		if first == nil || !first.Ignored || first.Rule != "a/**/b" {
			return fmt.Errorf("a/x/y/b should be ignored via a/**/b, got %+v", first)
		}
		// 零层 a/b 也命中。
		_, f2, _, _, _, err := checkEnd("a/**/b\n", []string{"a/b"})
		if err != nil {
			return err
		}
		if f2 == nil || !f2.Ignored {
			return fmt.Errorf("a/b should be ignored via a/**/b (zero level), got %+v", f2)
		}
		return nil
	})

	// ---- 锚定与基名差异（约束3）----
	check("锚定规则只从根匹配", func() error {
		status, first, _, _, errStr, err := checkEnd("/foo\n", []string{"bar/foo"})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		if first == nil || first.Ignored {
			return fmt.Errorf("bar/foo should be kept (/foo anchored), got %+v", first)
		}
		// foo 命中。
		_, f2, _, _, _, err := checkEnd("/foo\n", []string{"foo"})
		if err != nil {
			return err
		}
		if f2 == nil || !f2.Ignored || f2.Rule != "/foo" {
			return fmt.Errorf("foo should be ignored via /foo, got %+v", f2)
		}
		return nil
	})

	check("基名规则匹配任意层级", func() error {
		paths := []string{"foo", "a/foo", "a/foo/b"}
		status, _, _, _, errStr, err := checkEnd("foo\n", paths)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		// 三者都应被忽略（通过汇总列表校验：ignored 含全部三个）。
		_, _, ignored, _, _, err := checkEnd("foo\n", paths)
		if err != nil {
			return err
		}
		if len(ignored) != 3 {
			return fmt.Errorf("expected 3 ignored, got %v", ignored)
		}
		return nil
	})

	// ---- 目录规则递归忽略与专属性（约束4）----
	check("目录规则递归忽略叶子例外", func() error {
		// build/out.o 与 build 被忽略；src/build（叶子）保留。
		paths := []string{"build/out.o", "build", "src/build"}
		status, _, ignored, kept, errStr, err := checkEnd("build/\n", paths)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		hasIgn, isKept := findResult(kept, ignored, "build/out.o")
		if !hasIgn || isKept {
			return fmt.Errorf("build/out.o should be ignored, ignored=%v kept=%v", ignored, kept)
		}
		_, isKept2 := findResult(kept, ignored, "build")
		if isKept2 {
			return fmt.Errorf("build should be ignored, ignored=%v kept=%v", ignored, kept)
		}
		_, srcBuildKept := findResult(kept, ignored, "src/build")
		if !srcBuildKept {
			return fmt.Errorf("src/build (leaf) should be kept, ignored=%v kept=%v", ignored, kept)
		}
		return nil
	})

	// ---- 注释与转义 ----
	check("注释与转义字面量", func() error {
		rules := "# comment line\n\\#literal\n\\!bang\n"
		// #literal 命中 \#literal（非注释）。
		_, f1, _, _, _, err := checkEnd(rules, []string{"#literal"})
		if err != nil {
			return err
		}
		if f1 == nil || !f1.Ignored || f1.Rule != "\\#literal" || f1.Line != 2 {
			return fmt.Errorf("#literal should be ignored via \\#literal@2, got %+v", f1)
		}
		// !bang 命中 \!bang（非取反）。
		_, f2, _, _, _, err := checkEnd(rules, []string{"!bang"})
		if err != nil {
			return err
		}
		if f2 == nil || !f2.Ignored || f2.Rule != "\\!bang" || f2.Negated {
			return fmt.Errorf("!bang should be ignored via \\!bang@3 (not negated), got %+v", f2)
		}
		return nil
	})

	// ---- 空规则 / 空路径 ----
	check("空规则所有路径保留", func() error {
		status, _, _, kept, errStr, err := checkEnd("", []string{"a/b", "x.log"})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status=%d err=%s", status, errStr)
		}
		if len(kept) != 2 {
			return fmt.Errorf("empty rules should keep all, kept=%v", kept)
		}
		return nil
	})

	check("空路径列表返回空结果", func() error {
		body := marshal(map[string]any{"rules": "*.log", "paths": []string{}})
		resp, data, err := do(http.MethodPost, "/check", body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		if !strings.Contains(string(data), `"results":[]`) {
			return fmt.Errorf("expected results:[] in response, got: %s", data)
		}
		return nil
	})

	// ---- 路径规范化 ----
	check("前导斜杠被剥离", func() error {
		// /debug.log 剥离前导 / 后为 debug.log，命中 *.log。
		_, first, _, _, _, err := checkEnd("*.log\n", []string{"/debug.log"})
		if err != nil {
			return err
		}
		if first == nil || !first.Ignored || first.Path != "debug.log" {
			return fmt.Errorf("/debug.log should normalize to debug.log and be ignored, got %+v", first)
		}
		return nil
	})

	// ---- 请求格式校验 ----
	check("非法 JSON 被拒(400)", func() error {
		resp, _, err := do(http.MethodPost, "/check", "{not json")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})

	check("多段 JSON 被拒(400)", func() error {
		b1 := marshal(map[string]any{"rules": "", "paths": []string{"a"}})
		b2 := marshal(map[string]any{"rules": "", "paths": []string{"b"}})
		resp, _, err := do(http.MethodPost, "/check", b1+b2)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})

	check("未知字段被拒(400)", func() error {
		resp, _, err := do(http.MethodPost, "/check", marshal(map[string]any{"rules": "", "paths": []string{"a"}, "extra": 1}))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})

	check("空路径字符串被拒(400)", func() error {
		resp, _, err := do(http.MethodPost, "/check", marshal(map[string]any{"rules": "", "paths": []string{"a", ""}}))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})

	check("纯斜杠路径被拒(400)", func() error {
		resp, _, err := do(http.MethodPost, "/check", marshal(map[string]any{"rules": "", "paths": []string{"/"}}))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d want 400", resp.StatusCode)
		}
		return nil
	})

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}
