package screen

import "testing"

func TestElementsMatchByPageIDAndFocusWhateverTheLanguage(t *testing.T) {
	tree := ElementTree{Elements: []Element{
		{Role: "AXTextField", Title: "地址和搜索栏", Value: "chrome://extensions/"},
		{Role: "AXCheckBox", Title: "开发者模式", DOMID: "devMode"},
		{Role: "AXButton", Title: "加载未打包的扩展程序", DOMID: "loadUnpacked"},
		{Role: "AXTextField", Focused: true},
	}}
	cases := []struct {
		m    ElementMatch
		want int
	}{
		{ElementMatch{DOMID: "devMode"}, 2},
		{ElementMatch{DOMID: "loadUnpacked"}, 3},
		{ElementMatch{Role: "AXTextField", Focused: true}, 4},
		{ElementMatch{Focused: true}, 4},
	}
	for _, c := range cases {
		id, err := tree.match(c.m, true)
		if err != nil || id != c.want {
			t.Errorf("%s: [%d] %v, want [%d]", c.m, id, err, c.want)
		}
	}
	if _, err := tree.match(ElementMatch{DOMID: "packExtensions"}, false); err == nil {
		t.Error("an id no element has matched")
	}
	if got := (ElementMatch{Role: "AXButton", DOMID: "loadUnpacked", Focused: true}).String(); got != "Button #loadUnpacked (focused)" {
		t.Errorf("String = %q", got)
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
