package qqimage

import (
	"strings"
	"testing"
)

func TestReplyTemplateCatalogDefaultsRemainValid(t *testing.T) {
	catalog := ReplyTemplateCatalog()
	if len(catalog) != 26 {
		t.Fatalf("catalog=%d", len(catalog))
	}
	for _, def := range catalog {
		t.Run(def.Key, func(t *testing.T) {
			_, worst, errs := ValidateReplyTemplate(def.Key, def.DefaultText)
			if len(errs) != 0 || worst > ReplyTemplateMaxLength {
				t.Fatalf("default invalid: %v %d", errs, worst)
			}
			if preview := PreviewReplyTemplate(def.Key, def.DefaultText); !preview.Valid || preview.Text == "" {
				t.Fatalf("default preview invalid: %+v", preview)
			}
		})
	}
}

func TestReplyTemplateGrammarUTF16AndBudget(t *testing.T) {
	for _, text := range []string{"", "  \n", "\u200c\u200d \n", "{{unknown}}", "{{group_title}", "{group_title}", "{{ group_title }}", "{{group_title}}{{", "{{group_title}}\x00", "{{group_title}}\x7f", "{{group_title}}\u202e"} {
		if _, _, errs := ValidateReplyTemplate("iq.ack", text); len(errs) == 0 {
			t.Errorf("accepted %q", text)
		}
	}
	normalized, _, errs := ValidateReplyTemplate("iq.ack", "\r\n{{group_title}}\r\n")
	if len(errs) != 0 || strings.Contains(normalized, "\r") {
		t.Fatalf("normalization: %q %v", normalized, errs)
	}
	if _, _, errs := ValidateReplyTemplate("iq.ack", strings.Repeat("😀", 940)+"{{group_title}}"); len(errs) == 0 {
		t.Fatal("UTF16 expansion budget not enforced")
	}
	p := PreviewReplyTemplate("iq.ack", "😀{{group_title}}")
	if !p.Valid || p.Length != replyUTF16Length(p.Text) || p.MaxLength != 2000 {
		t.Fatalf("preview=%+v", p)
	}
	if _, _, errs := ValidateReplyTemplate("iq.ack", "👨‍👩‍👧‍👦👩‍💻{{group_title}}"); len(errs) != 0 {
		t.Fatalf("joined emoji rejected: %v", errs)
	}
	if got := replyUTF16Length("👨‍👩‍👧‍👦👩‍💻"); got != 16 {
		t.Fatalf("joined emoji length=%d", got)
	}
}

func TestReplyTemplateSinglePassFallbackAndNoSecrets(t *testing.T) {
	got := RenderReplyTemplate(map[string]string{"iq.ack": "已收到 {{group_title}}"}, "iq.ack", map[string]string{"group_title": "{{secret}}"})
	if got != "已收到 {{secret}}" {
		t.Fatalf("recursive render: %q", got)
	}
	got = RenderReplyTemplate(map[string]string{"iq.ack": "broken {{unknown}}"}, "iq.ack", map[string]string{"group_title": "月卡"})
	if got != "收到，正在查询并生成截图，请稍候。" || strings.Contains(got, "unknown") {
		t.Fatalf("default fallback: %q", got)
	}
	got = RenderReplyTemplate(nil, "iq.ack", map[string]string{"group_title": strings.Repeat("😀", 2000)})
	if replyUTF16Length(got) > 2000 || strings.Contains(got, "{{") {
		t.Fatalf("unsafe oversize fallback: %q", got)
	}
	for _, def := range ReplyTemplateCatalog() {
		for _, name := range def.AllowedVariables {
			for _, secret := range []string{"email", "openid", "confirmation_code", "prompt", "secret", "error_raw"} {
				if name == secret {
					t.Fatalf("secret variable %s", name)
				}
			}
		}
	}
}

func TestReplyTemplateBindingUsagePreviewMatchesLiveRules(t *testing.T) {
	def, ok := replyDefinitionFor("binding.usage")
	if !ok {
		t.Fatal("missing binding usage")
	}
	preview := PreviewReplyTemplate(def.Key, def.DefaultText)
	if !preview.Valid || preview.Text != "请使用 /绑定 发起本人申请。请勿替他人授权。" {
		t.Fatalf("preview differs from live usage: %+v", preview)
	}
}

func TestReplyTemplateAllDisplayFieldsOptional(t *testing.T) {
	for _, def := range ReplyTemplateCatalog() {
		t.Run(def.Key, func(t *testing.T) {
			if def.RequiredVariables == nil || len(def.RequiredVariables) != 0 {
				t.Errorf("display fields must all be optional: %v", def.RequiredVariables)
			}
			const custom = "欢迎来到糖果小站 👋\n请按需使用机器人。"
			if _, _, errs := ValidateReplyTemplate(def.Key, custom); len(errs) != 0 {
				t.Fatalf("plain administrator text rejected: %v", errs)
			}
			if got := RenderReplyTemplate(map[string]string{def.Key: custom}, def.Key, nil); got != custom {
				t.Fatalf("deleted information was appended: %q", got)
			}
			preview := PreviewReplyTemplate(def.Key, custom)
			if !preview.Valid || preview.Text != custom {
				t.Fatalf("plain text preview: %+v", preview)
			}
		})
	}
}

func TestReplyTemplateCaptionCanOmitTimezoneOrAllAutomaticFields(t *testing.T) {
	def, _ := replyDefinitionFor("iq.caption")
	withoutTimezone := strings.Replace(def.DefaultText, "（{{timezone}}）", "", 1)
	for _, text := range []string{withoutTimezone, "{{group_title}}", "本次查询结果如下。", "{{timezone}} · {{timezone}}"} {
		preview := PreviewReplyTemplate(def.Key, text)
		if !preview.Valid {
			t.Fatalf("optional caption fields rejected: %q %+v", text, preview)
		}
		if text == withoutTimezone && strings.Contains(preview.Text, "（北京时间）") {
			t.Fatalf("removed timezone returned: %q", preview.Text)
		}
	}
	if got := PreviewReplyTemplate(def.Key, "{{timezone}} · {{timezone}}").Text; got != "北京时间 · 北京时间" {
		t.Fatalf("repeated field rendered incorrectly: %q", got)
	}
	if _, _, errs := ValidateReplyTemplate("binding.request", strings.Repeat("{{binding_url}}", 2)); len(errs) != 0 {
		t.Fatalf("within-budget repetition rejected: %v", errs)
	}
	if _, _, errs := ValidateReplyTemplate("binding.request", strings.Repeat("{{binding_url}}", 3)); len(errs) == 0 {
		t.Fatal("repeated variable bypassed expanded length limit")
	}
}

func TestReplyTemplateEmptyExpansionPreviewAndRuntimeFallback(t *testing.T) {
	for _, text := range []string{"{{availability}}", "\n\u200d{{availability}} \u200c"} {
		preview := PreviewReplyTemplate("image.help", text)
		if preview.Valid || preview.Text != "" || preview.Length != 0 || len(preview.Errors) != 1 || preview.Errors[0] != "empty_rendered_text" {
			t.Fatalf("empty preview must be actionable: %+v", preview)
		}
	}
	def, _ := replyDefinitionFor("iq.ack")
	for _, value := range []string{"", " \n", "\u200d\u200c \n"} {
		if out, ok := renderReplyText(def, "{{group_title}}", map[string]string{"group_title": value}); ok {
			t.Fatalf("empty actual message accepted: %q", out)
		}
		got := RenderReplyTemplate(map[string]string{def.Key: "{{group_title}}"}, def.Key, map[string]string{"group_title": value})
		if got != def.DefaultText {
			t.Fatalf("empty actual message did not use default: %q", got)
		}
	}
	if out, ok := renderReplyText(def, "👩‍💻 {{group_title}}", map[string]string{"group_title": ""}); !ok || out != "👩‍💻 " {
		t.Fatalf("visible emoji incorrectly considered empty: %q %v", out, ok)
	}
}
