package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTryLockExcludesASecondHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if ok, err := TryLock(first); err != nil || !ok {
		t.Fatalf("first TryLock = %v, %v", ok, err)
	}
	if ok, err := TryLock(second); err != nil || ok {
		t.Fatalf("second TryLock while held = %v, %v; want false, nil", ok, err)
	}
	if err := Unlock(first); err != nil {
		t.Fatal(err)
	}
	if ok, err := TryLock(second); err != nil || !ok {
		t.Fatalf("TryLock after unlock = %v, %v", ok, err)
	}
	// The lock never blocks the file's own content.
	if _, err := first.WriteString("pid 1\n"); err != nil {
		t.Fatalf("write while another handle holds the lock: %v", err)
	}
}

func TestSyncDirOnARealDirectory(t *testing.T) {
	if err := SyncDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceFileSwapsAndSweepRemovesAsideCopies(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mewla.exe")
	staged := filepath.Join(dir, "mewla.exe.next")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(staged, target); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Fatalf("target = %q", got)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged still there: %v", err)
	}
	aside := target + replacedMarker + "1"
	unrelated := filepath.Join(dir, "other.exe.old-1")
	for _, path := range []string{aside, unrelated} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	SweepReplaced(target)
	if _, err := os.Stat(aside); !os.IsNotExist(err) {
		t.Fatalf("aside copy not swept: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("another program's file was swept: %v", err)
	}
}

func TestProcessAliveForThisProcess(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Fatal("this process reported dead")
	}
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Fatal("non-positive pid reported alive")
	}
}

func TestLogonTaskXML(t *testing.T) {
	task := LogonTask{
		Name: "Mewla", User: `PC\ana`, SID: "S-1-5-21-1",
		Command:     `C:\Users\ana\AppData\Local\Programs\Mewla\mewla.exe`,
		Arguments:   QuoteArgs([]string{"-state-dir", `C:\Users\ana b\.mewla`, "-addr", "127.0.0.1:9876"}),
		WorkDir:     `C:\Users\ana`,
		Description: "Mewla <daemon> & co",
	}
	text := task.XML()
	for _, want := range []string{
		`<LogonTrigger><Enabled>true</Enabled><UserId>PC\ana</UserId></LogonTrigger>`,
		`<UserId>S-1-5-21-1</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel>`,
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>`,
		`<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>`,
		`<Arguments>-state-dir &#34;C:\Users\ana b\.mewla&#34; -addr 127.0.0.1:9876</Arguments>`,
		`Mewla &lt;daemon&gt; &amp; co`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("task XML lacks %q:\n%s", want, text)
		}
	}
	file := UTF16File("A")
	if len(file) != 4 || file[0] != 0xff || file[1] != 0xfe || file[2] != 'A' || file[3] != 0 {
		t.Fatalf("UTF16File = %v", file)
	}
}

func TestQuoteArgs(t *testing.T) {
	cases := map[string][]string{
		`a b`:                  {"a", "b"},
		`"a b" c`:              {"a b", "c"},
		`""`:                   {""},
		`"a\"b"`:               {`a"b`},
		`"C:\dir with\\"`:      {`C:\dir with\`},
		`C:\plain\path.exe -x`: {`C:\plain\path.exe`, "-x"},
	}
	for want, args := range cases {
		if got := QuoteArgs(args); got != want {
			t.Errorf("QuoteArgs(%q) = %s, want %s", args, got, want)
		}
	}
}
