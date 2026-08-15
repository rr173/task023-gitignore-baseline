package gitignore

import (
	"reflect"
	"testing"
)

// mustParse 解析规则文本，遇错直接 fatal。
func mustParse(t *testing.T, text string) []Pattern {
	t.Helper()
	ps, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", text, err)
	}
	return ps
}

// assertIgnored 断言 path 在 patterns 下被忽略，且决定规则符合期望。
func assertIgnored(t *testing.T, ps []Pattern, path, wantRule string, wantLine int, wantNeg bool) {
	t.Helper()
	ignored, matched := Decide(ps, path)
	if !ignored {
		t.Errorf("path %q should be ignored", path)
		return
	}
	if matched == nil {
		t.Errorf("path %q ignored but no matched pattern", path)
		return
	}
	if matched.Source != wantRule {
		t.Errorf("path %q deciding rule=%q want %q", path, matched.Source, wantRule)
	}
	if matched.Line != wantLine {
		t.Errorf("path %q line=%d want %d", path, matched.Line, wantLine)
	}
	if matched.Negated != wantNeg {
		t.Errorf("path %q negated=%v want %v", path, matched.Negated, wantNeg)
	}
}

// assertKept 断言 path 被保留；wantRule 为空表示无命中，否则为最后命中的取反规则。
func assertKept(t *testing.T, ps []Pattern, path, wantRule string, wantLine int, wantNeg bool) {
	t.Helper()
	ignored, matched := Decide(ps, path)
	if ignored {
		t.Errorf("path %q should be kept", path)
		return
	}
	if wantRule == "" {
		if matched != nil {
			t.Errorf("path %q should have no match, got rule %q line %d", path, matched.Source, matched.Line)
		}
		return
	}
	if matched == nil {
		t.Errorf("path %q kept by negation but no matched pattern", path)
		return
	}
	if matched.Source != wantRule || matched.Line != wantLine || matched.Negated != wantNeg {
		t.Errorf("path %q decide rule=%q line=%d neg=%v want %q/%d/%v",
			path, matched.Source, matched.Line, matched.Negated, wantRule, wantLine, wantNeg)
	}
}

func TestParseEmptyAndComments(t *testing.T) {
	ps := mustParse(t, "# a comment\n\n   \n#another\n")
	if len(ps) != 0 {
		t.Fatalf("expected 0 patterns from comments/blank, got %d: %+v", len(ps), ps)
	}
}

func TestParseLineNumbers(t *testing.T) {
	ps := mustParse(t, "# header\nfoo\n\nbar\n")
	if len(ps) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(ps))
	}
	if ps[0].Source != "foo" || ps[0].Line != 2 {
		t.Errorf("ps[0]=%+v want foo@line2", ps[0])
	}
	if ps[1].Source != "bar" || ps[1].Line != 4 {
		t.Errorf("ps[1]=%+v want bar@line4", ps[1])
	}
}

func TestParseEscapedHashIsPattern(t *testing.T) {
	// \# 开头是字面 # 规则，非注释。
	ps := mustParse(t, "\\#literal\n")
	if len(ps) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(ps))
	}
	if ps[0].Source != "\\#literal" {
		t.Errorf("source=%q want %q", ps[0].Source, "\\#literal")
	}
	assertIgnored(t, ps, "#literal", "\\#literal", 1, false)
	assertKept(t, ps, "other", "", 0, false)
}

func TestParseEscapedBangIsLiteral(t *testing.T) {
	// \! 开头是字面 ! 规则，非取反。
	ps := mustParse(t, "\\!bang\n")
	if len(ps) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(ps))
	}
	if ps[0].Negated {
		t.Error("\\! should not be negation")
	}
	assertIgnored(t, ps, "!bang", "\\!bang", 1, false)
}

func TestParseNegation(t *testing.T) {
	ps := mustParse(t, "!keep\n")
	if len(ps) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(ps))
	}
	if !ps[0].Negated {
		t.Error("! prefix should set Negated")
	}
	if ps[0].Source != "!keep" {
		t.Errorf("source=%q want !keep", ps[0].Source)
	}
}

func TestParseTrailingSlashDirOnly(t *testing.T) {
	ps := mustParse(t, "build/\n")
	if len(ps) != 1 || !ps[0].DirOnly {
		t.Fatalf("expected dir-only pattern, got %+v", ps)
	}
}

func TestParseAnchoring(t *testing.T) {
	cases := []struct {
		rule     string
		anchored bool
	}{
		{"/foo", true},   // 行首 / → 锚定
		{"a/b", true},    // 含内部 / → 锚定
		{"a/b/", true},   // 含内部 / → 锚定（且目录）
		{"foo", false},   // 无 / → 基名
		{"foo/", false},  // 仅行尾 / → 基名（目录）
		{"*.log", false}, // 无 / → 基名
	}
	for _, c := range cases {
		ps := mustParse(t, c.rule+"\n")
		if len(ps) != 1 {
			t.Errorf("rule %q: expected 1 pattern, got %d", c.rule, len(ps))
			continue
		}
		if ps[0].Anchored != c.anchored {
			t.Errorf("rule %q: anchored=%v want %v", c.rule, ps[0].Anchored, c.anchored)
		}
	}
}

func TestLastMatchWinsBasic(t *testing.T) {
	ps := mustParse(t, "*.log\n!keep.log\n")
	// keep.log 被取反规则重新包含。
	assertKept(t, ps, "keep.log", "!keep.log", 2, true)
	// debug.log 仍被普通规则忽略。
	assertIgnored(t, ps, "debug.log", "*.log", 1, false)
	// 任意层级下的 .log 文件被忽略。
	assertIgnored(t, ps, "src/app.log", "*.log", 1, false)
	// 非 .log 保留（无命中）。
	assertKept(t, ps, "src/main.go", "", 0, false)
}

func TestLastMatchWinsReIgnore(t *testing.T) {
	// * 忽略全部；!a 保留 a；a 又把 a 重新忽略（最后命中决定）。
	ps := mustParse(t, "*\n!a\na\n")
	assertIgnored(t, ps, "a", "a", 3, false)
	assertIgnored(t, ps, "b", "*", 1, false)
	// c 仅命中 *（忽略），不命中 !a 或 a。
	assertIgnored(t, ps, "c", "*", 1, false)
}

func TestStarDoesNotCrossSlash(t *testing.T) {
	// a/*.txt 只命中 a 的直接子级 .txt 文件。
	ps := mustParse(t, "a/*.txt\n")
	assertIgnored(t, ps, "a/notes.txt", "a/*.txt", 1, false)
	assertKept(t, ps, "a/sub/notes.txt", "", 0, false) // * 不能跨越 sub/
	// a 本身不命中（不是 .txt）。
	assertKept(t, ps, "a", "", 0, false)
}

func TestDoubleStarCrossesLevels(t *testing.T) {
	ps := mustParse(t, "a/**/b\n")
	// 零层：a/b
	assertIgnored(t, ps, "a/b", "a/**/b", 1, false)
	// 多层：a/x/y/b
	assertIgnored(t, ps, "a/x/y/b", "a/**/b", 1, false)
	// b 不是独立段时不命中：a/xb
	assertKept(t, ps, "a/xb", "", 0, false)
}

func TestDoubleStarLeading(t *testing.T) {
	ps := mustParse(t, "**/logs\n")
	assertIgnored(t, ps, "logs", "**/logs", 1, false)
	assertIgnored(t, ps, "a/logs", "**/logs", 1, false)
	assertKept(t, ps, "logsx", "", 0, false)
}

func TestDoubleStarTrailing(t *testing.T) {
	ps := mustParse(t, "foo/**\n")
	assertIgnored(t, ps, "foo/x", "foo/**", 1, false)
	assertIgnored(t, ps, "foo/x/y/z", "foo/**", 1, false)
	// foo 自身不命中（foo/** 只匹配 foo/ 之下）。
	assertKept(t, ps, "foo", "", 0, false)
}

func TestAnchoredVsBasename(t *testing.T) {
	// /foo 锚定根：命中 foo，不命中 bar/foo。
	anchored := mustParse(t, "/foo\n")
	assertIgnored(t, anchored, "foo", "/foo", 1, false)
	assertIgnored(t, anchored, "foo/bar", "/foo", 1, false) // 根级 foo 为目录，级联
	assertKept(t, anchored, "bar/foo", "", 0, false)

	// foo 基名：命中任意层级的 foo 段。
	basename := mustParse(t, "foo\n")
	assertIgnored(t, basename, "foo", "foo", 1, false)
	assertIgnored(t, basename, "a/foo", "foo", 1, false)
}

func TestDirOnlyRecursiveAndSpecificity(t *testing.T) {
	ps := mustParse(t, "build/\n")
	// 目录级联：build/ 下全部忽略。
	assertIgnored(t, ps, "build/out.o", "build/", 1, false)
	assertIgnored(t, ps, "build", "build/", 1, false) // 根级目录本身
	// 同名叶子文件不被目录规则忽略。
	assertKept(t, ps, "src/build", "", 0, false)
	// 任意层级的 build 目录也被忽略。
	assertIgnored(t, ps, "a/build/deep.o", "build/", 1, false)
}

func TestNonDirBasenameMatchesLeaf(t *testing.T) {
	// 非目录规则 build 命中叶子文件 src/build。
	ps := mustParse(t, "build\n")
	assertIgnored(t, ps, "src/build", "build", 1, false)
	assertIgnored(t, ps, "build", "build", 1, false)
}

func TestQuestionMark(t *testing.T) {
	ps := mustParse(t, "a?c\n")
	assertIgnored(t, ps, "abc", "a?c", 1, false)
	assertKept(t, ps, "ac", "", 0, false)   // ? 需要恰好一个非 / 字符
	assertKept(t, ps, "a/c", "", 0, false)  // ? 不匹配 /
}

func TestEscapedStarIsLiteral(t *testing.T) {
	// \* 为字面星号。
	ps := mustParse(t, "a\\*b\n")
	assertIgnored(t, ps, "a*b", "a\\*b", 1, false)
	assertKept(t, ps, "aXb", "", 0, false)
}

func TestNoMatchKept(t *testing.T) {
	ps := mustParse(t, "*.tmp\nnode_modules/\n")
	assertIgnored(t, ps, "x.tmp", "*.tmp", 1, false)
	assertIgnored(t, ps, "node_modules/x", "node_modules/", 2, false)
	assertKept(t, ps, "src/main.go", "", 0, false)
}

func TestCheckBatch(t *testing.T) {
	ps := mustParse(t, "*.log\n!keep.log\n")
	paths := []string{"debug.log", "keep.log", "src/main.go"}
	results := Check(ps, paths)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	want := []Result{
		{Path: "debug.log", Ignored: true, Rule: "*.log", Line: 1, Negated: false},
		{Path: "keep.log", Ignored: false, Rule: "!keep.log", Line: 2, Negated: true},
		{Path: "src/main.go", Ignored: false, Rule: "", Line: 0, Negated: false},
	}
	for i, w := range want {
		if !reflect.DeepEqual(results[i], w) {
			t.Errorf("results[%d] = %+v, want %+v", i, results[i], w)
		}
	}
}

func TestCheckEmptyPaths(t *testing.T) {
	ps := mustParse(t, "*.log\n")
	results := Check(ps, nil)
	if len(results) != 0 {
		t.Errorf("empty paths should give empty results, got %d", len(results))
	}
}

func TestDecideEmptyRules(t *testing.T) {
	// 空规则 → 所有路径保留。
	ps := mustParse(t, "")
	if len(ps) != 0 {
		t.Fatalf("expected 0 patterns, got %d", len(ps))
	}
	ignored, matched := Decide(ps, "anything")
	if ignored {
		t.Error("empty rules should keep all paths")
	}
	if matched != nil {
		t.Error("empty rules should have no matched pattern")
	}
}

func TestMatchAllDoubleStar(t *testing.T) {
	// ** 全匹配。
	ps := mustParse(t, "**\n")
	assertIgnored(t, ps, "anything", "**", 1, false)
	assertIgnored(t, ps, "a/b/c", "**", 1, false)
}

func TestCRLFHandled(t *testing.T) {
	// CRLF 行尾应被正确剥离。
	ps := mustParse(t, "foo\r\nbar\r\n")
	if len(ps) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(ps))
	}
	if ps[0].Source != "foo" {
		t.Errorf("ps[0].Source=%q want foo (no CR)", ps[0].Source)
	}
	if ps[1].Source != "bar" {
		t.Errorf("ps[1].Source=%q want bar (no CR)", ps[1].Source)
	}
}
