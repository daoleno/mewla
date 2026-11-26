package screen

import "testing"

func TestElementsMatchByFocus(t *testing.T) {
	tree := ElementTree{Elements: []Element{
		{Role: "AXTextField", Title: "地址和搜索栏", Value: "chrome://extensions/"},
		{Role: "AXTextField", Focused: true},
	}}
	for _, m := range []ElementMatch{{Role: "AXTextField", Focused: true}, {Focused: true}} {
		if id, err := tree.match(m, true); err != nil || id != 2 {
			t.Errorf("%s: [%d] %v, want [2]", m, id, err)
		}
	}
	if got := (ElementMatch{Role: "AXTextField", Focused: true}).String(); got != "TextField (focused)" {
		t.Errorf("String = %q", got)
	}
}

// His Chrome's extensions page in Chinese (11-26), as Mewla reads it while
// Chrome thinks a screen reader reads it: found by order, not by its words.
func TestTheExtensionsToolbarIsFoundByItsOrderInAnyLanguage(t *testing.T) {
	page := func(address, devMode string) ElementTree {
		return ElementTree{Elements: []Element{
			{Role: "AXWindow", Depth: 0},
			{Role: "AXTextField", Title: "地址和搜索栏", Value: address, Depth: 8},
			{Role: "AXPopUpButton", Title: "扩展程序", Depth: 8},
			{Role: "AXWebArea", Title: "扩展程序", Depth: 8},
			{Role: "AXButton", Title: "主菜单", Depth: 12},
			{Role: "AXHeading", Title: "扩展程序", Depth: 12},
			{Role: "AXTextField", Title: "搜索扩展程序", Depth: 13},
			{Role: "AXStaticText", Value: "开发者模式", Depth: 13},
			{Role: "AXCheckBox", Title: "开发者模式", Value: devMode, Depth: 13},
			{Role: "AXButton", Title: "加载未打包的扩展程序", Depth: 13},
			{Role: "AXButton", Title: "打包扩展程序", Depth: 13},
			{Role: "AXCheckBox", Title: "开启，扩展程序已启用", Value: "1", Depth: 14},
			{Role: "AXWindow", Depth: 0},
			{Role: "AXTextField", Value: "https://mail.example.com/", Depth: 8},
		}}
	}
	got, err := readExtensionsToolbar(page("chrome://extensions/", "1"))
	if err != nil || got != (extensionsToolbar{devMode: 9, loadUnpacked: 10, developerMode: true}) {
		t.Fatalf("in Developer mode: %+v %v", got, err)
	}
	got, err = readExtensionsToolbar(page("chrome://extensions/?id=keclgeaemhlgnnhocopjklenhgfimmjh", "0"))
	if err != nil || got != (extensionsToolbar{devMode: 9}) {
		t.Fatalf("Developer mode off: %+v %v", got, err)
	}
	if _, err := readExtensionsToolbar(page("https://example.com/", "1")); err == nil {
		t.Fatal("another page's switch was taken for Developer mode")
	}
	tree := page("chrome://extensions/", "1")
	tree.Elements = tree.Elements[:3]
	if _, err := readExtensionsToolbar(tree); err == nil {
		t.Fatal("a window that shows no page has a toolbar")
	}
	// Typed, not yet opened: the person's new tab page still shows, with
	// switches and buttons of its own.
	typed := page("chrome://extensions/", "1")
	typed.Elements[1].Focused = true
	if _, err := readExtensionsToolbar(typed); err == nil {
		t.Fatal("the page before the extensions page was read as it")
	}
	// The window in front shows no page: another window's is not its.
	behind := page("https://mail.example.com/", "1")
	behind.Elements = append(behind.Elements[12:], behind.Elements[:12]...)
	behind.Elements[1].Value = "chrome://extensions/"
	behind.Elements[3].Value = "chrome://extensions/"
	if _, err := readExtensionsToolbar(behind); err == nil {
		t.Fatal("a window behind lent the one in front its page")
	}
	if a := frontAddress(behind); a != "chrome://extensions/" {
		t.Fatalf("front address %q", a)
	}
}

// The open panel's Go to Folder field, read from his Chrome (11-26): the
// window's own address bar comes first and may say it is focused too.
func TestTheGoToFieldIsTheFocusedFieldOfTheLastSheet(t *testing.T) {
	tree := ElementTree{Elements: []Element{
		{Role: "AXWindow", Depth: 0},
		{Role: "AXTextField", Title: "地址和搜索栏", Focused: true, Depth: 4},
		{Role: "AXSheet", Title: "打开", Depth: 1},
		{Role: "AXCell", Focused: true, Depth: 5},
		{Role: "AXTextField", Depth: 3},
		{Role: "AXSheet", Depth: 2},
		{Role: "AXButton", Title: "关闭", Depth: 3},
		{Role: "AXTextField", Value: "/Applications/Kairo.app", Focused: true, Depth: 3},
	}}
	if id, err := focusedFieldInLastSheet(tree); err != nil || id != 8 {
		t.Fatalf("[%d] %v", id, err)
	}
	if _, err := focusedFieldInLastSheet(ElementTree{Elements: tree.Elements[:5]}); err == nil {
		t.Fatal("the panel's own search field is not the Go to field")
	}
}

// Owner's Mac, 11-26: his new tab page takes the keyboard from a new
// window's address bar; Mewla's window is the one that was not there before.
func TestMewlasWindowIsTheOneThatWasNotThereBefore(t *testing.T) {
	tree := ElementTree{
		Elements: []Element{
			{Role: "AXWindow", Depth: 0},
			{Role: "AXButton", Title: "返回", Depth: 7},
			{Role: "AXTextField", Title: "地址和搜索栏", Depth: 8},
			{Role: "AXWindow", Depth: 0},
			{Role: "AXTextField", Title: "地址和搜索栏", Value: "news.ycombinator.com", Depth: 8},
		},
		IDs: []int{40, 41, 42, 10, 11},
	}
	if window, address, err := newWindowAddress(tree, map[int]bool{10: true}); err != nil || window != 40 || address != 42 {
		t.Fatalf("[%d] [%d] %v", window, address, err)
	}
	if _, _, err := newWindowAddress(tree, map[int]bool{40: true, 10: true}); err == nil {
		t.Fatal("a window that was there was taken for Mewla's")
	}
}

// Mewla's window closes by its own close button: the window in front on the
// extensions page while Chrome has one more window than before; never
// another.
func TestOnlyMewlasWindowCloses(t *testing.T) {
	tree := ElementTree{Elements: []Element{
		{Role: "AXWindow", Depth: 0},
		{Role: "AXTextField", Value: "chrome://extensions", Depth: 8},
		{Role: "AXButton", Subrole: "AXCloseButton", Depth: 1},
		{Role: "AXWindow", Depth: 0},
		{Role: "AXTextField", Value: "news.ycombinator.com", Depth: 8},
		{Role: "AXButton", Subrole: "AXCloseButton", Depth: 1},
	}}
	if id, err := closeButtonOfMewlas(tree, 1); err != nil || id != 3 {
		t.Fatalf("[%d] %v", id, err)
	}
	if _, err := closeButtonOfMewlas(tree, 2); err == nil {
		t.Fatal("closed a window while Chrome had no more windows than before")
	}
	tree.Elements[1].Value = "https://mail.example.com/"
	if _, err := closeButtonOfMewlas(tree, 1); err == nil {
		t.Fatal("closed a window that shows another page")
	}
}
