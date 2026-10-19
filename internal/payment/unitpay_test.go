package payment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Пример из документации UnitPay: params[b]=bob, params[c]=sam, params[a]=tod,
// ключ a1b1c1d1 → sha256("check{up}tod{up}bob{up}sam{up}a1b1c1d1").
func TestUnitPaySignatureMatchesDocExample(t *testing.T) {
	sum := sha256.Sum256([]byte("check{up}tod{up}bob{up}sam{up}a1b1c1d1"))
	want := hex.EncodeToString(sum[:])
	params := map[string]string{"b": "bob", "c": "sam", "a": "tod"}
	if got := UnitPaySignature("check", params, "a1b1c1d1"); got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	params["signature"] = want
	if !UnitPayVerify("check", params, "a1b1c1d1") {
		t.Fatal("verify отклонил верную подпись (signature не должна входить в склейку)")
	}
	params["a"] = "tom"
	if UnitPayVerify("check", params, "a1b1c1d1") {
		t.Fatal("verify принял подпись подменённых параметров")
	}
}

func withUnitPayAPI(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	prev := UnitPayAPIBase
	UnitPayAPIBase = srv.URL
	t.Cleanup(func() { UnitPayAPIBase = prev; srv.Close() })
}

func TestUnitPayInitPaymentSendsParamsAndParsesNumericID(t *testing.T) {
	withUnitPayAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for k, want := range map[string]string{
			"method":              "initPayment",
			"params[account]":     "ord_1",
			"params[sum]":         "25.00",
			"params[currency]":    "USD",
			"params[projectId]":   "445942",
			"params[secretKey]":   "sk",
			"params[paymentType]": "card",
			"params[test]":        "1",
			"params[login]":       "me@example.com",
		} {
			if got := q.Get(k); got != want {
				t.Errorf("%s = %q, want %q", k, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"result":{"type":"redirect","paymentId":1231231234,"redirectUrl":"https://unitpay.money/pay/x"}}`))
	})
	u := UnitPay{ProjectID: "445942", SecretKey: "sk", Test: true, Login: "me@example.com"}
	res, err := u.InitPayment(UnitPayInitRequest{Account: "ord_1", Sum: "25.00", Currency: "USD", Desc: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	if res.PaymentID != "1231231234" || res.RedirectURL != "https://unitpay.money/pay/x" {
		t.Fatalf("res = %+v", res)
	}
}

func TestUnitPayAPIErrorIsTyped(t *testing.T) {
	withUnitPayAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"message":"Неверный ключ secretKey","code":-32000}}`))
	})
	_, err := UnitPay{ProjectID: "1", SecretKey: "bad"}.GetPayment("42")
	var apiErr *UnitPayAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != -32000 {
		t.Fatalf("err = %v, want UnitPayAPIError -32000", err)
	}
}

func TestUnitPayGetPaymentParsesStringFields(t *testing.T) {
	withUnitPayAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"paymentId":"2188481996","status":"success","account":"ord_1","orderSum":"25.00","orderCurrency":"USD","paymentType":"card"}}`))
	})
	p, err := UnitPay{ProjectID: "1", SecretKey: "sk"}.GetPayment("2188481996")
	if err != nil {
		t.Fatal(err)
	}
	want := UnitPayPayment{PaymentID: "2188481996", Status: "success", Account: "ord_1", OrderSum: "25.00", OrderCurrency: "USD", PaymentType: "card"}
	if p != want {
		t.Fatalf("p = %+v, want %+v", p, want)
	}
}
