package knowledge

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuiltinKnowledgeSearchReadAndTopics(t *testing.T) {
	b := NewBuiltin()
	matches, err := b.Search("PowerShell 5.1", "public", "windows", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 || !strings.HasPrefix(matches[0].Path, "public/windows/") {
		t.Fatalf("matches=%#v", matches)
	}
	read, err := b.Read(matches[0].Path, 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if read.Lines == 0 || !strings.Contains(read.Content, "PowerShell") {
		t.Fatalf("read=%#v", read)
	}
	topics, err := b.Topics("public")
	if err != nil || len(topics) < 5 {
		t.Fatalf("topics=%#v err=%v", topics, err)
	}
	seenTopics := make(map[string]bool, len(topics))
	for _, topic := range topics {
		seenTopics[topic.Topic] = true
	}
	if !seenTopics["android"] || seenTopics["git"] || seenTopics["containers"] {
		t.Fatalf("unexpected public topic ownership: %#v", topics)
	}
	adb, err := b.Search("ADB USB Wi-Fi pairing", "public", "android", 3)
	if err != nil || len(adb) == 0 || adb[0].Path != "public/android/adb.md" {
		t.Fatalf("adb search=%#v err=%v", adb, err)
	}
	adbRead, err := b.Read("public/android/adb.md", 1, 120)
	if err != nil || !strings.Contains(adbRead.Content, "adb pair") || !strings.Contains(adbRead.Content, "adb -d tcpip 5555") {
		t.Fatalf("adb read=%#v err=%v", adbRead, err)
	}
	lilith, err := b.Search("Lilith module documentation conventions", "public", "lilith", 3)
	if err != nil || len(lilith) == 0 || lilith[0].Path != "public/lilith/architecture.md" {
		t.Fatalf("lilith search=%#v err=%v", lilith, err)
	}
}

func TestPrivateNamespaceRegistrationAndTraversalGuard(t *testing.T) {
	ns := "companytest"
	err := RegisterNamespace(ns, fstest.MapFS{"runbooks/deploy.md": {Data: []byte("# Private deploy\ncompany only")}})
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuiltin()
	matches, err := b.Search("company only", ns, "", 2)
	if err != nil || len(matches) != 1 || matches[0].Path != ns+"/runbooks/deploy.md" {
		t.Fatalf("matches=%#v err=%v", matches, err)
	}
	if _, err := b.Read("../secret.md", 1, 10); err == nil {
		t.Fatal("path traversal accepted")
	}
}
