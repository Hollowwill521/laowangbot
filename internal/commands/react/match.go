package react

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dlclark/regexp2"
)

// 关键词的写法跟 monitor 完全一致：普通词忽略大小写做包含匹配，`re:` 开头按
// ECMAScript 正则，支持 `re:/表达式/flags`。
//
// 这是从 plugins/monitor/matching.go 抄过来的，不是抽了个公共包：抽包要动上游
// 自有的 monitor 代码，每次 rebase 都得多解一处冲突，而这里要的只有这四十来行。
// 改 monitor 的关键词语义时，记得同步这里。
type matcher struct {
	mu       sync.Mutex
	compiled map[string]*regexp2.Regexp
	rejected map[string]bool
}

func newMatcher() *matcher {
	return &matcher{compiled: map[string]*regexp2.Regexp{}, rejected: map[string]bool{}}
}

// match 返回第一个命中的关键词条目。命中哪条要报出来：表情是跟着词走的，
// 而且日志里看不到词，事后就没法判断是这个词太宽还是群设错了。
func (m *matcher) match(text string, keywords []Keyword) (Keyword, bool) {
	for _, keyword := range keywords {
		if m.matches(text, keyword.Text) {
			return keyword, true
		}
	}
	return Keyword{}, false
}

func (m *matcher) matches(text, keyword string) bool {
	pattern, err := m.compile(keyword)
	if err != nil {
		return false
	}
	if pattern == nil {
		return strings.Contains(strings.ToLower(text), strings.ToLower(strings.TrimSpace(keyword)))
	}
	ok, err := pattern.MatchString(text)
	return err == nil && ok
}

// compile 编译并缓存 `re:` 关键词。
//
// 缓存是为了编译一次：这两处都在每条经过的消息上用，monitor 那边每次现编译，
// 一个群每分钟几十上百条消息时正则编译就是白烧 CPU。编译失败的词也记下来，
// 免得坏表达式跟着每条消息重试；新增关键词会整个清掉缓存，所以改了也能立刻生效。
func (m *matcher) compile(keyword string) (*regexp2.Regexp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pattern, ok := m.compiled[keyword]; ok {
		return pattern, nil
	}
	if m.rejected[keyword] {
		return nil, fmt.Errorf("关键词 %s 的正则无效", keyword)
	}
	pattern, err := compileKeyword(keyword)
	if err != nil {
		m.rejected[keyword] = true
		return nil, err
	}
	m.compiled[keyword] = pattern
	return pattern, nil
}

// reset 丢掉编译缓存，改过关键词表之后调用。
func (m *matcher) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compiled = map[string]*regexp2.Regexp{}
	m.rejected = map[string]bool{}
}

// validateKeyword 在存盘之前检查写法，参数错误就该当场报错而不是悄悄存下来。
func validateKeyword(keyword string) error {
	if strings.TrimSpace(keyword) == "" {
		return fmt.Errorf("关键词不能为空")
	}
	_, err := compileKeyword(keyword)
	return err
}

// compileKeyword 把关键词写成正则。非正则关键词返回 nil，表示按包含匹配。
func compileKeyword(keyword string) (*regexp2.Regexp, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	if !strings.HasPrefix(strings.ToLower(keyword), "re:") {
		return nil, nil
	}
	expression := strings.TrimSpace(keyword[3:])
	options := regexp2.RegexOptions(regexp2.ECMAScript | regexp2.IgnoreCase)
	if strings.HasPrefix(expression, "/") {
		end := strings.LastIndex(expression, "/")
		if end <= 0 {
			return nil, fmt.Errorf("正则表达式无效")
		}
		flags := expression[end+1:]
		expression = expression[1:end]
		options = regexp2.ECMAScript
		seen := map[rune]bool{}
		for _, flag := range flags {
			if seen[flag] {
				return nil, fmt.Errorf("重复正则标记")
			}
			seen[flag] = true
			switch flag {
			case 'i':
				options |= regexp2.IgnoreCase
			case 'm':
				options |= regexp2.Multiline
			case 's':
				options |= regexp2.Singleline
			case 'y':
				expression = `\A(?:` + expression + `)`
			case 'g', 'u', 'd':
			case 'v':
				return nil, fmt.Errorf("暂不支持 JS v Unicode 集合标记")
			default:
				return nil, fmt.Errorf("无效正则标记")
			}
		}
	}
	if expression == "" {
		return nil, fmt.Errorf("正则表达式不能为空")
	}
	pattern, err := regexp2.Compile(expression, options)
	if err == nil {
		// 一条坏正则能把更新流拖死，100ms 上限和 monitor 保持一致。
		pattern.MatchTimeout = 100 * time.Millisecond
	}
	return pattern, err
}
