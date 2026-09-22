package workloom

// INSTALL.md 是 agent 可读的安装器（README 方式 A 提示词只指向它）。
// 这个测试照 i-have-adhd 的 test_install_docs.py 思路，防止文档腐烂：
// 安装/接入流程里的关键命令与安全门禁必须一直在文档里，措辞可以改，
// 阶段不能 silently 丢。

import (
	"os"
	"strings"
	"testing"
)

func TestInstallDocCoversTheFlow(t *testing.T) {
	data, err := os.ReadFile("INSTALL.md")
	if err != nil {
		t.Fatalf("read INSTALL.md: %v", err)
	}
	doc := string(data)

	// §1 安装（机器级）：取脚本 → 校验 → 执行 → 自证。
	for _, want := range []string{
		"releases/latest",
		"checksums.txt",
		"sha256sum install.sh",
		"Get-FileHash install.ps1",
		"install.sh --tag",
		"workloom --version",
		"gh auth login",
		"go install github.com/JAYY513/Workloom/cmd/workloom",
		"GOPRIVATE",
		"GOFLAGS=-mod=vendor",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("INSTALL.md lacks %q (install stage incomplete or wording drifted)", want)
		}
	}
	// Default go install is the public module. GOPRIVATE skips the checksum
	// database and may appear only as a private-fork note, not in §1b's command.
	start := strings.Index(doc, "### 1b.")
	next := strings.Index(doc, "### 1c.")
	if start < 0 || next < start {
		t.Fatal("INSTALL.md §1b/§1c headings missing")
	}
	if strings.Contains(doc[start:next], "go env -w GOPRIVATE") {
		t.Error("INSTALL.md §1b still sets GOPRIVATE on the default go install path")
	}
	if !strings.Contains(doc, "公开仓库") {
		t.Error("INSTALL.md does not state that this repository is public")
	}

	// §2 接入（项目级）：目录身份 + 接入链 + 蓝图纪律。Git 是可选能力。
	for _, want := range []string{
		"目标项目目录",
		"非 Git 原型可直接 init",
		"workloom init",
		"workflow init --template",
		"workloom wire",
		"wire --check",
		"workloom prime",
		"--blueprint-artifact",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("INSTALL.md lacks %q (onboarding stage incomplete or wording drifted)", want)
		}
	}
}
