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
		"devsys --version",
		"gh auth login",
		"go install github.com/JAYY513/Workloom/cmd/devsys",
		"GOPRIVATE",
		"GOFLAGS=-mod=vendor",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("INSTALL.md lacks %q (install stage incomplete or wording drifted)", want)
		}
	}

	// §2 接入（项目级）：目录身份 + 接入链 + 蓝图纪律。Git 是可选能力。
	for _, want := range []string{
		"目标项目目录",
		"非 Git 原型可直接 init",
		"devsys init",
		"workflow init --template",
		"devsys wire",
		"wire --check",
		"devsys prime",
		"--blueprint-artifact",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("INSTALL.md lacks %q (onboarding stage incomplete or wording drifted)", want)
		}
	}
}
