// Package knowledge implements the knowledge layer's page contract and the
// bookkeeping that keeps the layer honest about which code it describes
// (方案 §12): the front matter every page carries, located validation, and the
// scan/freshness machinery the later milestones build on.
//
// The layer has two halves (方案 §12.6). The index layer is ours: it is
// scanned from the project and can be rebuilt at will. The page layer is
// written by an external generator — RepoWiki is the preferred optional one —
// so pages are an interop surface: unknown front matter keys are warnings, not
// errors, and `sources` may live in the generator's state mapping instead of
// the page.
package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/JAYY513/Workloom/internal/config"
)

// Layout constants. The page layer is wherever a generator writes it; these
// are the locations this system knows by name.
const (
	// Dir is the knowledge layer's home under .devsys/ (index layer and
	// bookkeeping).
	Dir = ".devsys/knowledge"
	// DefaultRepowikiRoot is the RepoWiki bundle's knowledge cards: the
	// preferred optional generator's page layer (方案 §12.6).
	DefaultRepowikiRoot = "docs/repowiki/knowledge"
	// DefaultPagesRoot is where a generator that follows the contract writes
	// pages directly.
	DefaultPagesRoot = ".devsys/knowledge/pages"
	// SnapshotFile is the index-layer snapshot (M5.2).
	SnapshotFile = ".devsys/knowledge/snapshot.json"
	// StateFile holds the freshness baseline and the page mapping (M5.3).
	StateFile = ".devsys/knowledge/state.json"
	// RunFile is the generator checkpoint that makes a refresh resumable
	// (M5.5).
	RunFile = ".devsys/knowledge/run.json"
)

// DefaultRoots are the page roots a command reads when the project does not
// configure `knowledge_pages`. RepoWiki comes first because it is the
// preferred optional generator; a root that does not exist is skipped by the
// caller rather than reported.
var DefaultRoots = []string{DefaultRepowikiRoot, DefaultPagesRoot}

// AuthoredStatuses are the statuses a page may declare. `stale` and `invalid`
// are deliberately absent: the freshness check (方案 §12.5) and this validator
// compute them, and a hand-written claim about a computed state is a lie the
// layer cannot check.
var AuthoredStatuses = []string{"current", "stable", "draft", "unverified", "conflicted", "deprecated", "archived"}

// KnownTypes carry a defined meaning here. Other lowercase tokens are accepted
// with a warning: generators have their own vocabulary (RepoWiki's dimensions,
// for one) and the contract must not pretend to be closed.
var KnownTypes = []string{"module", "overview", "architecture", "api", "data_model", "config", "test", "process", "guide", "reference", "note"}

// KnownKeys is the front matter this contract defines. Anything else is a
// warning: pages come from external generators, so unknown keys are an interop
// fact, not a defect.
var KnownKeys = []string{
	"status", "type", "dimension", "triggers", "description", "generated",
	"generator", "source_commit", "sources", "content_hash", "protected",
}

// RequiredKeys must be present (their values are checked separately).
var RequiredKeys = []string{"status", "type", "triggers", "description", "source_commit"}

const (
	fence  = "---"
	fence2 = "..."
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	hashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	typePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	drivePattern  = regexp.MustCompile(`^[A-Za-z]:`)
)

// Page is one Markdown knowledge page: front matter plus body. Fields that
// failed to parse keep their zero value and the matching problem is reported.
type Page struct {
	// Path is the page's location, slash separated and relative to the
	// project root.
	Path string

	Status       string
	Type         string
	Dimension    string
	Triggers     []string
	Description  string
	SourceCommit string
	Sources      []string
	ContentHash  string
	Generator    string
	Generated    bool
	Protected    bool

	// Keys lists every top-level front matter key in file order.
	Keys []string
	// Body is the Markdown body; BodyLine is the 1-based line where it starts
	// (the line after the closing fence).
	Body     string
	BodyLine int
}

// Hash returns the sha256 of the page's body, hex encoded. The content hash
// the layer records for hand-edit protection is defined over the whole file
// (M5.4); this is the body's identity, used to compare what a generator wrote.
func (p *Page) Hash() string {
	sum := sha256.Sum256([]byte(p.Body))
	return hex.EncodeToString(sum[:])
}

// Parse reads one page. Problems are located (`file:line: field: reason`);
// a problem without a severity is an error, and a page with errors cannot be
// trusted. The page is returned even when it has errors, carrying whatever
// could be read.
func Parse(path string, data []byte) (*Page, []config.Problem) {
	p := &Page{Path: path}
	var problems []config.Problem

	if !utf8.Valid(data) {
		return p, []config.Problem{errAt(path, 1, "", "文件不是合法 UTF-8")}
	}
	if bom := strings.HasPrefix(string(data), "\ufeff"); bom {
		data = data[len("\ufeff"):]
		problems = append(problems, warnAt(path, 1, "", "文件带 UTF-8 BOM；请以无 BOM 的 UTF-8 保存"))
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || !isFence(lines[0], fence) {
		problems = append(problems, errAt(path, 1, "", "缺少 front matter：页面必须以 `---` 单独一行开始"))
		return p, problems
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if isFence(lines[i], fence) || isFence(lines[i], fence2) {
			end = i
			break
		}
	}
	if end < 0 {
		problems = append(problems, errAt(path, 1, "", "front matter 未闭合：缺少结束的 `---`"))
		return p, problems
	}

	// Front matter occupies file lines 2..end (1-based); the YAML node lines
	// are relative to that block, so a node on block line N is on file line
	// N+1.
	block := strings.Join(lines[1:end], "\n")
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(block), &root); err != nil {
		problems = append(problems, errAt(path, blockLine(err)+1, "", "front matter 不是合法 YAML：%s", yamlMessage(err)))
		return p, problems
	}
	if root.Kind == 0 { // empty document
		problems = append(problems, errAt(path, 1, "", "front matter 为空：缺少 %s", strings.Join(RequiredKeys, "、")))
		return p, problems
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		problems = append(problems, errAt(path, doc.Line+1, "", "front matter 必须是键值映射"))
		return p, problems
	}

	seen := map[string]*yaml.Node{}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key, val := doc.Content[i], doc.Content[i+1]
		name := key.Value
		p.Keys = append(p.Keys, name)
		if _, dup := seen[name]; dup {
			problems = append(problems, errAt(path, key.Line+1, name, "重复的键"))
			continue
		}
		seen[name] = val
		if !known(name) {
			problems = append(problems, warnAt(path, key.Line+1, name, "未知字段（外部生成器的扩展字段允许存在；请确认拼写）"))
		}
	}

	for _, name := range RequiredKeys {
		if _, ok := seen[name]; !ok {
			problems = append(problems, errAt(path, 1, name, "缺少必填字段"))
		}
	}

	p.Status = p.checkStatus(path, seen, &problems)
	p.Type, p.Dimension = p.checkType(path, seen, &problems)
	p.Triggers = p.checkTriggers(path, seen, &problems)
	p.Description = p.checkDescription(path, seen, &problems)
	p.SourceCommit = p.checkSourceCommit(path, seen, &problems)
	p.Sources = p.checkSources(path, seen, &problems)
	p.ContentHash = p.checkHash(path, seen, &problems)
	p.Generator, p.Generated = p.checkGenerator(path, seen, &problems)
	p.Protected = p.checkProtected(path, seen, &problems)

	p.BodyLine = end + 2
	p.Body = strings.Join(lines[end+1:], "\n")
	if strings.TrimSpace(p.Body) == "" {
		problems = append(problems, errAt(path, end+1, "", "正文为空：页面只有 front matter"))
	} else if !strings.Contains(p.Body, "\n# ") && !strings.HasPrefix(strings.TrimSpace(p.Body), "# ") {
		problems = append(problems, warnAt(path, p.BodyLine, "", "正文缺少一级标题"))
	}
	return p, problems
}

func (p *Page) checkStatus(path string, seen map[string]*yaml.Node, problems *[]config.Problem) string {
	node, ok := seen["status"]
	if !ok {
		return ""
	}
	value, problem := scalar(path, node, "status")
	if problem != nil {
		*problems = append(*problems, *problem)
		return ""
	}
	switch {
	case value == "stale" || value == "invalid":
		*problems = append(*problems, errAt(path, node.Line+1, "status",
			"%q 是计算得出的状态（新鲜度检查、校验器），不能手写；请写 %s", value, strings.Join(AuthoredStatuses, "/")))
	case !contains(AuthoredStatuses, value):
		*problems = append(*problems, errAt(path, node.Line+1, "status",
			"未知状态 %q；允许 %s", value, strings.Join(AuthoredStatuses, "/")))
	}
	return value
}

func (p *Page) checkType(path string, seen map[string]*yaml.Node, problems *[]config.Problem) (string, string) {
	var typ string
	if node, ok := seen["type"]; ok {
		value, problem := scalar(path, node, "type")
		switch {
		case problem != nil:
			*problems = append(*problems, *problem)
		case !typePattern.MatchString(value):
			*problems = append(*problems, errAt(path, node.Line+1, "type",
				"类型必须是 [a-z][a-z0-9_]* 的小写标记，得到 %q", value))
		default:
			typ = value
			if !contains(KnownTypes, value) {
				*problems = append(*problems, warnAt(path, node.Line+1, "type", "未登记的类型 %q（允许；请确认不是拼写错误）", value))
			}
		}
	}
	var dimension string
	if node, ok := seen["dimension"]; ok {
		value, problem := scalar(path, node, "dimension")
		if problem != nil {
			*problems = append(*problems, *problem)
		} else {
			dimension = value
		}
	}
	return typ, dimension
}

func (p *Page) checkTriggers(path string, seen map[string]*yaml.Node, problems *[]config.Problem) []string {
	node, ok := seen["triggers"]
	if !ok {
		return nil
	}
	list, problem := scalarList(path, node, "triggers")
	if problem != nil {
		*problems = append(*problems, *problem)
		return nil
	}
	if len(list) == 0 {
		*problems = append(*problems, errAt(path, node.Line+1, "triggers", "必须是非空列表：没有触发词的页面不会被加载"))
	}
	checkEntries(path, node, "triggers", list, problems)
	return list
}

func (p *Page) checkDescription(path string, seen map[string]*yaml.Node, problems *[]config.Problem) string {
	node, ok := seen["description"]
	if !ok {
		return ""
	}
	value, problem := scalar(path, node, "description")
	if problem != nil {
		*problems = append(*problems, *problem)
		return ""
	}
	if strings.TrimSpace(value) == "" {
		*problems = append(*problems, errAt(path, node.Line+1, "description", "不能为空"))
		return ""
	}
	return value
}

func (p *Page) checkSourceCommit(path string, seen map[string]*yaml.Node, problems *[]config.Problem) string {
	node, ok := seen["source_commit"]
	if !ok {
		return ""
	}
	value, problem := scalar(path, node, "source_commit")
	if problem != nil {
		*problems = append(*problems, *problem)
		return ""
	}
	switch {
	case value == "":
		*problems = append(*problems, warnAt(path, node.Line+1, "source_commit",
			"未记录基线提交；新鲜度检查会把该页视为未基线"))
	case !commitPattern.MatchString(value):
		*problems = append(*problems, errAt(path, node.Line+1, "source_commit",
			"必须是 7-40 位小写十六进制提交号（得到 %q）", value))
	}
	return value
}

func (p *Page) checkSources(path string, seen map[string]*yaml.Node, problems *[]config.Problem) []string {
	node, ok := seen["sources"]
	if !ok {
		// 方案 §12.5 keeps the page-to-source mapping in the generator's state
		// as well as in the page: without a mapping, declaration here is the
		// only way the layer can attribute changes to this page.
		*problems = append(*problems, warnAt(path, 1, "sources",
			"页面内未声明 sources；需由生成器 state 映射提供（方案 §12.5）"))
		return nil
	}
	list, problem := scalarList(path, node, "sources")
	if problem != nil {
		*problems = append(*problems, *problem)
		return nil
	}
	if len(list) == 0 {
		*problems = append(*problems, errAt(path, node.Line+1, "sources", "必须是非空列表"))
	}
	checkEntries(path, node, "sources", list, problems)
	return list
}

func (p *Page) checkHash(path string, seen map[string]*yaml.Node, problems *[]config.Problem) string {
	node, ok := seen["content_hash"]
	if !ok {
		return ""
	}
	value, problem := scalar(path, node, "content_hash")
	if problem != nil {
		*problems = append(*problems, *problem)
		return ""
	}
	if value != "" && !hashPattern.MatchString(value) {
		*problems = append(*problems, errAt(path, node.Line+1, "content_hash", "必须是 64 位小写十六进制 sha256（得到 %q）", value))
	}
	return value
}

func (p *Page) checkGenerator(path string, seen map[string]*yaml.Node, problems *[]config.Problem) (string, bool) {
	var name string
	generated := false
	if node, ok := seen["generator"]; ok {
		value, problem := scalar(path, node, "generator")
		if problem != nil {
			*problems = append(*problems, *problem)
		} else {
			name = value
		}
	}
	if node, ok := seen["generated"]; ok {
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
			*problems = append(*problems, errAt(path, node.Line+1, "generated", "必须是布尔值"))
			return name, false
		}
		generated = node.Value == "true"
	}
	return name, generated
}

func (p *Page) checkProtected(path string, seen map[string]*yaml.Node, problems *[]config.Problem) bool {
	node, ok := seen["protected"]
	if !ok {
		return false
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		*problems = append(*problems, errAt(path, node.Line+1, "protected", "必须是布尔值"))
		return false
	}
	return node.Value == "true"
}

// checkEntries validates list entries that are shared by triggers and sources:
// non-empty strings, unique, and — for source patterns — project-relative.
func checkEntries(path string, node *yaml.Node, field string, list []string, problems *[]config.Problem) {
	seen := map[string]bool{}
	for i, entry := range list {
		line := node.Line + 1
		if node.Kind == yaml.SequenceNode && i < len(node.Content) {
			line = node.Content[i].Line + 1
		}
		if strings.TrimSpace(entry) == "" {
			*problems = append(*problems, errAt(path, line, field, "第 %d 项为空", i+1))
			continue
		}
		if entry != strings.TrimSpace(entry) {
			*problems = append(*problems, warnAt(path, line, field, "第 %d 项含首尾空白（匹配会失效）", i+1))
		}
		if seen[entry] {
			*problems = append(*problems, warnAt(path, line, field, "第 %d 项重复：%q", i+1, entry))
		}
		seen[entry] = true
		if field == "sources" {
			if problem := checkSourcePattern(path, line, entry); problem != nil {
				*problems = append(*problems, *problem)
			}
		}
	}
}

// checkSourcePattern enforces the shape a source pattern must have to be
// matchable later: project-relative, slash separated, no parent traversal.
func checkSourcePattern(path string, line int, pattern string) *config.Problem {
	switch {
	case strings.HasPrefix(pattern, "/"):
		return problemPtr(errAt(path, line, "sources", "%q 必须是项目内相对路径，不能以 / 开头", pattern))
	case drivePattern.MatchString(pattern):
		return problemPtr(errAt(path, line, "sources", "%q 必须是项目内相对路径，不能带盘符", pattern))
	case strings.Contains(pattern, "\\"):
		return problemPtr(errAt(path, line, "sources", "%q 必须用 / 分隔", pattern))
	case containsSegment(pattern, ".."):
		return problemPtr(errAt(path, line, "sources", "%q 不能包含 .. 段", pattern))
	case containsSegment(pattern, ""):
		return problemPtr(errAt(path, line, "sources", "%q 含空路径段", pattern))
	}
	return nil
}

// scalar reads one required string value.
func scalar(path string, node *yaml.Node, field string) (string, *config.Problem) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", problemPtr(errAt(path, node.Line+1, field, "必须是字符串，得到 %s", nodeKind(node)))
	}
	return node.Value, nil
}

// scalarList reads one list of strings, rejecting nested structures and
// non-string entries.
func scalarList(path string, node *yaml.Node, field string) ([]string, *config.Problem) {
	if node.Kind != yaml.SequenceNode {
		return nil, problemPtr(errAt(path, node.Line+1, field, "必须是字符串列表，得到 %s", nodeKind(node)))
	}
	list := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			return nil, problemPtr(errAt(path, item.Line+1, field, "第 %d 项必须是字符串，得到 %s", len(list)+1, nodeKind(item)))
		}
		list = append(list, item.Value)
	}
	return list, nil
}

// blockLine extracts the line a YAML parse error points at, relative to the
// front matter block; 0 means the message carried none, and the problem then
// belongs to the front matter opener.
func blockLine(err error) int {
	msg := err.Error()
	i := strings.Index(msg, "line ")
	if i < 0 {
		return 0
	}
	n := 0
	for _, r := range msg[i+len("line "):] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// yamlMessage keeps the first line of a YAML error: the rest is the parser's
// position report, which the located problem already carries.
func yamlMessage(err error) string {
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), "yaml: "))
	lines := strings.Split(msg, "\n")
	first := strings.TrimSpace(lines[0])
	first = strings.TrimSuffix(first, ":")
	return first
}

func isFence(line, want string) bool {
	return strings.TrimRight(line, " \t\r") == want
}

func known(name string) bool { return contains(KnownKeys, name) }

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func containsSegment(pattern, segment string) bool {
	for _, part := range strings.Split(pattern, "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func nodeKind(node *yaml.Node) string {
	switch node.Kind {
	case yaml.MappingNode:
		return "映射"
	case yaml.SequenceNode:
		return "列表"
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			return "null"
		}
		return fmt.Sprintf("标量（%s）", strings.TrimPrefix(node.Tag, "!!"))
	default:
		return "空"
	}
}

func errAt(path string, line int, field, format string, a ...any) config.Problem {
	return config.Problem{File: path, Line: line, Field: field, Reason: fmt.Sprintf(format, a...)}
}

func warnAt(path string, line int, field, format string, a ...any) config.Problem {
	p := errAt(path, line, field, format, a...)
	p.Severity = config.SeverityWarning
	return p
}

func problemPtr(p config.Problem) *config.Problem { return &p }
