package openai

import "testing"

func TestGetHTTPClientReusesSameProxyClient(t *testing.T) {
	client1, err := getHTTPClient("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("getHTTPClient returned error: %v", err)
	}

	client2, err := getHTTPClient("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("getHTTPClient returned error: %v", err)
	}

	if client1 != client2 {
		t.Fatal("expected same proxy URL to reuse cached client")
	}
}

func TestGetHTTPClientSeparatesDifferentProxyClients(t *testing.T) {
	client1, err := getHTTPClient("")
	if err != nil {
		t.Fatalf("getHTTPClient returned error: %v", err)
	}

	client2, err := getHTTPClient("http://127.0.0.1:7891")
	if err != nil {
		t.Fatalf("getHTTPClient returned error: %v", err)
	}

	if client1 == client2 {
		t.Fatal("expected different proxy settings to use different clients")
	}
}
