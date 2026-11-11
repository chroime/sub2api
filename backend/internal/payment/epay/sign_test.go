package epay

import "testing"

func TestSignMatchesEasyPayCanonicalMD5(t *testing.T) {
	params := map[string]string{"pid": "1", "type": "alipay", "money": "10.00", "out_trade_no": "ORDER_1", "empty": "", "sign": "ignored", "sign_type": "MD5"}
	// md5("money=10.00&out_trade_no=ORDER_1&pid=1&type=alipay" + "merchant-key").
	want := "a6d165f6d41afc1919313981227a845e"
	if got := Sign(params, "merchant-key"); got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	if !Verify(params, "merchant-key", want) || Verify(params, "wrong", want) {
		t.Fatal("signature authentication did not bind merchant key")
	}
	params["money"] = "10.01"
	if Verify(params, "merchant-key", want) {
		t.Fatal("altered amount retained a valid signature")
	}
}
