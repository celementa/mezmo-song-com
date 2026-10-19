package marketing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSubscribeSendsListAndFields(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subscribe" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = r.ParseForm()
		got = r.PostForm
		_, _ = w.Write([]byte(`{"result":{"person_id":1}}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", ListID: "1", APIBase: srv.URL}
	if err := c.Subscribe(context.Background(), " User@Mail.ru ", map[string]string{FieldOrderPaid: "1"}); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"api_key": "k", "list_ids": "1", "fields[email]": "user@mail.ru",
		"fields[order_pay]": "1", "double_optin": "3", "overwrite": "2", "format": "json",
	} {
		if v := got.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
}

func TestSubscribeReportsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"invalid api key","code":"invalid_api_key"}`))
	}))
	defer srv.Close()
	c := &Client{APIKey: "bad", ListID: "1", APIBase: srv.URL}
	if err := c.Subscribe(context.Background(), "a@b.ru", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestDisabledClientIsNoop(t *testing.T) {
	var c *Client
	if err := c.Subscribe(context.Background(), "a@b.ru", nil); err != nil {
		t.Fatal(err)
	}
}
