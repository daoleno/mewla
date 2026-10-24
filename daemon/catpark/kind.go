// Package catpark is the daemon side of the cat's little life: the cat leaves
// when Brain's delegated Work starts, sends a few postcards while it is away,
// comes home with a souvenir when the Work is accepted, and tends a small yard.
//
// It invents no state. Trips open and close from Brain's real Work, the cat
// waits when something needs the person (the same signal as the live
// notification), and every postcard and souvenir is keyed by Work ID and
// persisted before it is announced, so a daemon restart repeats nothing.
package catpark

import (
	"strings"
	"unicode"
)

// Kind is the sort of place a Work sends the cat to. It picks the postcard
// picture, the souvenir and the line, and nothing else: a Work's title never
// reaches a postcard.
type Kind string

const (
	KindStudio      Kind = "studio"
	KindHarbour     Kind = "harbour"
	KindLibrary     Kind = "library"
	KindWorkshop    Kind = "workshop"
	KindNight       Kind = "night"
	KindGarden      Kind = "garden"
	KindObservatory Kind = "observatory"
	KindMarket      Kind = "market"
	KindKitchen     Kind = "kitchen"
	KindPostOffice  Kind = "postoffice"
	KindStation     Kind = "station"
	KindTrail       Kind = "trail"
)

// Kinds lists every kind in matching priority; the last is the fallback.
var Kinds = []Kind{
	KindWorkshop, KindHarbour, KindStudio, KindLibrary, KindGarden, KindObservatory,
	KindMarket, KindKitchen, KindPostOffice, KindStation, KindNight, KindTrail,
}

// A keyword starting with "=" must equal a whole word. Any other Latin keyword
// matches the start of a word ("profil" finds "profiling"; "read" does not
// find "thread"). A CJK keyword matches anywhere: it has no word boundaries.
var kindKeywords = map[Kind][]string{
	KindWorkshop:    {"fix", "bug", "repair", "debug", "patch", "refactor", "lint", "flaky", "crash", "=test", "testing", "修", "故障", "重构", "测试", "排查"},
	KindHarbour:     {"release", "deploy", "ship", "publish", "package", "rollout", "harbo", "parcel", "cargo", "freight", "=ci", "=cd", "=tag", "发版", "发布", "部署", "打包", "上线"},
	KindStudio:      {"design", "logo", "image", "illustrat", "paint", "draw", "icon", "mockup", "figma", "=ui", "=ux", "animation", "设计", "画", "图", "视觉", "动画"},
	KindLibrary:     {"research", "survey", "study", "paper", "docs", "documentation", "read", "investigat", "explore", "调研", "研究", "文档", "阅读", "调查"},
	KindGarden:      {"data", "database", "=db", "=sql", "etl", "pipeline", "dataset", "schema", "index", "postgres", "mysql", "sqlite", "数据", "数据库", "报表"},
	KindObservatory: {"monitor", "review", "audit", "metric", "benchmark", "performance", "profil", "observ", "监控", "审查", "评审", "审计", "性能"},
	KindMarket:      {"price", "pricing", "trade", "trading", "finance", "invoice", "billing", "cost", "budget", "buy", "shop", "stock", "交易", "价格", "财务", "采购", "账单"},
	KindKitchen:     {"infra", "server", "docker", "kube", "config", "setup", "install", "cron", "=ops", "运维", "配置", "安装", "服务器"},
	KindPostOffice:  {"email", "mail", "message", "notify", "notification", "slack", "telegram", "calendar", "reply", "邮件", "消息", "通知", "日历", "回复"},
	KindStation:     {"migrat", "move", "sync", "upload", "download", "transfer", "import", "export", "迁移", "同步", "上传", "导出", "导入"},
	KindNight:       {"write", "draft", "blog", "essay", "article", "report", "post", "copy", "写", "文章", "草稿", "报告", "文案"},
}

// KindOf maps a Work title to a kind with fixed keywords. It is deterministic,
// offline and leak-free: only the kind comes out of the title.
func KindOf(title string) Kind {
	lower := strings.ToLower(title)
	words := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	for _, kind := range Kinds {
		for _, keyword := range kindKeywords[kind] {
			if whole, ok := strings.CutPrefix(keyword, "="); ok {
				if set[whole] {
					return kind
				}
			} else if matchesKeyword(lower, words, keyword) {
				return kind
			}
		}
	}
	return KindTrail
}

func matchesKeyword(lower string, words []string, keyword string) bool {
	if isLatin(keyword) {
		for _, word := range words {
			if strings.HasPrefix(word, keyword) {
				return true
			}
		}
		return false
	}
	return strings.Contains(lower, keyword)
}

func isLatin(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// souvenirItems is the shelf piece each kind brings home.
var souvenirItems = map[Kind]string{
	KindStudio: "palette", KindHarbour: "bottle", KindLibrary: "book", KindWorkshop: "wrench",
	KindNight: "lantern", KindGarden: "pot", KindObservatory: "telescope", KindMarket: "bell",
	KindKitchen: "teapot", KindPostOffice: "stamp", KindStation: "compass", KindTrail: "pinecone",
}

// SouvenirItem is the souvenir a kind brings home.
func SouvenirItem(kind Kind) string {
	if item, ok := souvenirItems[kind]; ok {
		return item
	}
	return souvenirItems[KindTrail]
}
