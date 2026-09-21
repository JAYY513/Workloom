package workloom

// README 是 60 秒决策页：方式 A 提示词（指向 INSTALL.md）、方式 B 速查、
// 模板清单单源化指针、使用手册指针。这个测试守护这些结构性元素存在——
// 措辞可以改，指针断了必须红。

import (
	"os"
	"strings"
	"testing"
)

func TestReadmeKeepsDecisionPageShape(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	zh := string(readme)

	// 方式 A：提示词块指向 INSTALL.md 的两段式流程；init 身份是项目目录。
	for _, want := range []string{
		"帮我在当前项目接入 Workloom",
		"INSTALL.md 的",
		"目标项目目录执行 init",
		"非 Git 原型可直接 init",
	} {
		if !strings.Contains(zh, want) {
			t.Errorf("README.md (zh) lacks %q (option A prompt broken)", want)
		}
	}

	// 方式 B：权威流程指向 INSTALL.md，安装命令形态在。
	for _, want := range []string{
		"完整安装与接入流程（含校验、回退与排障）见 [INSTALL.md]",
		"bash install.sh --tag",
		"-File install.ps1 -Tag",
		"完整模板列表见该命令的用法输出", // 模板清单单源化
		"见《使用手册》",
	} {
		if !strings.Contains(zh, want) {
			t.Errorf("README.md (zh) lacks %q", want)
		}
	}

	en, err := os.ReadFile("docs/README.en.md")
	if err != nil {
		t.Fatalf("read docs/README.en.md: %v", err)
	}
	ens := string(en)
	for _, want := range []string{
		"Help me set up Workloom",
		"INSTALL.md",
		"bash install.sh --tag",
		"-File install.ps1 -Tag",
		"full list: the command's own usage output",
		"lives in the handbook",
	} {
		if !strings.Contains(ens, want) {
			t.Errorf("docs/README.en.md lacks %q", want)
		}
	}
}
