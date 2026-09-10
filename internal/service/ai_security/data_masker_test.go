package ai_security

import "testing"

// TestMaskFunctions 校验各掩码函数，重点覆盖短值不 panic（历史隐患）。
func TestMaskFunctions(t *testing.T) {
	cases := []struct {
		fn   string
		in   string
		want string
	}{
		{"phone", "13812345678", "138****5678"},
		{"phone", "1", "****"}, // 短值不再 panic
		{"email", "a@b.com", "***@b.com"},
		{"email", "ab@b.com", "***@b.com"},
		{"email", "abc@b.com", "a***@b.com"},
		{"email", "no-at-sign", "***@no-at-sign"},
		{"id_card", "110101199001011234", "1101****1234"},
		{"id_card", "1", "****"}, // 短值不再 panic
		{"bank_card", "6222021234567890", "6222****7890"},
		{"bank_card", "", "****"},
		{"hash", "secret-value", ""}, // 任意非空哈希
	}
	for _, c := range cases {
		got := applyMask(c.fn, c.in)
		if c.fn == "hash" {
			if len(got) != 8 {
				t.Errorf("applyMask(hash, %q)=%q, want 8-char hash", c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("applyMask(%s, %q)=%q, want %q", c.fn, c.in, got, c.want)
		}
	}
}

// TestGetMaskFunc 校验默认内置规则匹配（无 DB 时走内置规则）。
func TestGetMaskFunc(t *testing.T) {
	cases := []struct{ col, want string }{
		{"user_phone", "phone"},
		{"mobile_no", "phone"},
		{"email_address", "email"},
		{"user_id_card_no", "id_card"},
		{"login_password", "hash"},
		{"bank_card_no", "bank_card"},
		{"monthly_salary", "hash"},
		{"plain_column", ""},
	}
	for _, c := range cases {
		if got := getMaskFunc(c.col); got != c.want {
			t.Errorf("getMaskFunc(%q)=%q, want %q", c.col, got, c.want)
		}
	}
}
