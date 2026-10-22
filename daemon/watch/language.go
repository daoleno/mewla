package watch

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// What Watch says to the person itself, outside what Brain writes. Anything
// else it hands to Brain or the judge, which write in the person's language.
// A language missing here gets English.
type phrases struct {
	briefTitle, briefNotes, urgentTitle string
}

var phrasebook = map[string]phrases{
	"en": {
		briefTitle:  "Morning brief",
		briefNotes:  "Watch's brief of the problems nobody is handling. Move or repeat it to change when it comes; cancel it to turn it off.",
		urgentTitle: "Needs you now",
	},
	"zh": {
		briefTitle:  "早报",
		briefNotes:  "Watch 汇总没人处理的问题。改时间或重复方式就改变早报的时间,取消就关闭早报。",
		urgentTitle: "现在需要你",
	},
}

func phrasesFor(code string) phrases {
	if p, ok := phrasebook[code]; ok {
		return p
	}
	return phrasebook["en"]
}

// languageOf is the language code of a locale such as "zh-Hans-CN" or
// "pt_BR"; empty when the locale means nothing.
func languageOf(locale string) string {
	locale = strings.ReplaceAll(strings.TrimSpace(locale), "_", "-")
	if locale == "" || len(locale) > 35 {
		return ""
	}
	tag, err := language.Parse(locale)
	if err != nil {
		return ""
	}
	if tag == language.Und {
		return ""
	}
	base, _ := tag.Base()
	if code := base.String(); code != "und" {
		return code
	}
	return ""
}

// languageName is the English name of a language code, for the judge's
// instructions.
func languageName(code string) string {
	tag, err := language.Parse(code)
	if err != nil {
		return "English"
	}
	if name := display.English.Languages().Name(tag); name != "" {
		return name
	}
	return "English"
}
