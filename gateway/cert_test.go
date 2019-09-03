package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/TykTechnologies/tyk/apidef"
	"github.com/TykTechnologies/tyk/certs"
	"github.com/TykTechnologies/tyk/config"
	"github.com/TykTechnologies/tyk/test"
	"github.com/TykTechnologies/tyk/user"
)

const (
	internalTLSErr = "tls: internal error"
	badcertErr     = "tls: bad certificate"
)

func TestGatewayTLS(t *testing.T) {
	// Configure server
	serverCertPem, serverPrivPem, combinedPEM, _ := test.GenServerCertificate()

	dir, _ := ioutil.TempDir("", "certs")
	defer os.RemoveAll(dir)

	client := GetTLSClient(nil, nil)

	t.Run("Without certificates", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.HttpServerOptions.UseSSL = true
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
		})

		ts.Run(t, test.TestCase{ErrorMatch: internalTLSErr, Client: client})
	})

	t.Run("Legacy TLS certificate path", func(t *testing.T) {
		certFilePath := filepath.Join(dir, "server.crt")
		ioutil.WriteFile(certFilePath, serverCertPem, 0666)

		certKeyPath := filepath.Join(dir, "server.key")
		ioutil.WriteFile(certKeyPath, serverPrivPem, 0666)

		globalConf := config.Global()
		globalConf.HttpServerOptions.Certificates = []config.CertData{{
			Name:     "localhost",
			CertFile: certFilePath,
			KeyFile:  certKeyPath,
		}}
		globalConf.HttpServerOptions.UseSSL = true
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
		})

		ts.Run(t, test.TestCase{Code: 200, Client: client})

		CertificateManager.FlushCache()
	})

	t.Run("File certificate path", func(t *testing.T) {
		certPath := filepath.Join(dir, "server.pem")
		ioutil.WriteFile(certPath, combinedPEM, 0666)

		globalConf := config.Global()
		globalConf.HttpServerOptions.SSLCertificates = []string{certPath}
		globalConf.HttpServerOptions.UseSSL = true
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
		})

		ts.Run(t, test.TestCase{Code: 200, Client: client})

		CertificateManager.FlushCache()
	})

	t.Run("Redis certificate", func(t *testing.T) {
		certID, err := CertificateManager.Add(combinedPEM, "")
		if err != nil {
			t.Fatal(err)
		}
		defer CertificateManager.Delete(certID)

		globalConf := config.Global()
		globalConf.HttpServerOptions.SSLCertificates = []string{certID}
		globalConf.HttpServerOptions.UseSSL = true
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
		})

		ts.Run(t, test.TestCase{Code: 200, Client: client})

		CertificateManager.FlushCache()
	})
}

func TestGatewayControlAPIMutualTLS(t *testing.T) {
	// Configure server
	serverCertPem, _, combinedPEM, _ := test.GenServerCertificate()

	// make global config changes
	globalConf := config.Global()
	globalConf.HttpServerOptions.UseSSL = true
	config.SetGlobal(globalConf)
	defer ResetTestConfig()

	dir, _ := ioutil.TempDir("", "certs")

	defer func() {
		os.RemoveAll(dir)
		CertificateManager.FlushCache()
	}()

	clientCertPem, _, _, clientCert := test.GenCertificate(&x509.Certificate{})
	clientWithCert := GetTLSClient(&clientCert, serverCertPem)

	clientWithoutCert := GetTLSClient(nil, nil)

	t.Run("Separate domain", func(t *testing.T) {
		certID, _ := CertificateManager.Add(combinedPEM, "")
		defer CertificateManager.Delete(certID)

		globalConf := config.Global()
		globalConf.ControlAPIHostname = "localhost"
		globalConf.HttpServerOptions.SSLCertificates = []string{certID}
		globalConf.Security.Certificates.ControlAPI = []string{certID}
		config.SetGlobal(globalConf)

		ts := StartTest()
		defer ts.Close()

		defer func() {
			CertificateManager.FlushCache()
			globalConf := config.Global()
			globalConf.HttpServerOptions.SSLCertificates = nil
			globalConf.Security.Certificates.ControlAPI = nil
			config.SetGlobal(globalConf)
		}()

		unknownErr := "x509: certificate signed by unknown authority"
		badcertErr := "tls: bad certificate"

		ts.Run(t, []test.TestCase{
			// Should acess tyk without client certificates
			{Client: clientWithoutCert},
			// Should raise error for ControlAPI without certificate
			{ControlRequest: true, ErrorMatch: unknownErr},
			// Should raise error for for unknown certificate
			{ControlRequest: true, ErrorMatch: badcertErr, Client: clientWithCert},
		}...)

		clientCertID, _ := CertificateManager.Add(clientCertPem, "")
		defer CertificateManager.Delete(clientCertID)

		globalConf = config.Global()
		globalConf.Security.Certificates.ControlAPI = []string{clientCertID}
		config.SetGlobal(globalConf)

		// Should pass request with valid client cert
		ts.Run(t, test.TestCase{
			Path: "/tyk/certs", Code: 200, ControlRequest: true, AdminAuth: true, Client: clientWithCert,
		})
	})
}

func TestAPIMutualTLS(t *testing.T) {
	// Configure server
	serverCertPem, _, combinedPEM, _ := test.GenServerCertificate()
	certID, _ := CertificateManager.Add(combinedPEM, "")
	defer CertificateManager.Delete(certID)

	globalConf := config.Global()
	globalConf.EnableCustomDomains = true
	globalConf.HttpServerOptions.UseSSL = true
	globalConf.ListenPort = 0
	globalConf.HttpServerOptions.SSLCertificates = []string{certID}
	config.SetGlobal(globalConf)
	defer ResetTestConfig()

	ts := StartTest()
	defer ts.Close()

	// Initialize client certificates
	clientCertPem, _, _, clientCert := test.GenCertificate(&x509.Certificate{})

	t.Run("SNI and domain per API", func(t *testing.T) {
		t.Run("API without mutual TLS", func(t *testing.T) {
			client := GetTLSClient(&clientCert, serverCertPem)

			BuildAndLoadAPI(func(spec *APISpec) {
				spec.Domain = "localhost"
				spec.Proxy.ListenPath = "/"
			})

			ts.Run(t, test.TestCase{Path: "/", Code: 200, Client: client, Domain: "localhost"})
		})

		t.Run("MutualTLSCertificate not set", func(t *testing.T) {
			client := GetTLSClient(nil, nil)

			BuildAndLoadAPI(func(spec *APISpec) {
				spec.Domain = "localhost"
				spec.Proxy.ListenPath = "/"
				spec.UseMutualTLSAuth = true
			})

			ts.Run(t, test.TestCase{
				ErrorMatch: badcertErr,
				Client:     client,
				Domain:     "localhost",
			})
		})

		t.Run("Client certificate match", func(t *testing.T) {
			client := GetTLSClient(&clientCert, serverCertPem)
			clientCertID, _ := CertificateManager.Add(clientCertPem, "")

			BuildAndLoadAPI(func(spec *APISpec) {
				spec.Domain = "localhost"
				spec.Proxy.ListenPath = "/"
				spec.UseMutualTLSAuth = true
				spec.ClientCertificates = []string{clientCertID}
			})

			ts.Run(t, test.TestCase{
				Code: 200, Client: client, Domain: "localhost",
			})

			CertificateManager.Delete(clientCertID)
			CertificateManager.FlushCache()

			client = GetTLSClient(&clientCert, serverCertPem)
			ts.Run(t, test.TestCase{
				Client: client, Domain: "localhost", ErrorMatch: badcertErr,
			})
		})

		t.Run("Client certificate differ", func(t *testing.T) {
			client := GetTLSClient(&clientCert, serverCertPem)

			clientCertPem2, _, _, _ := test.GenCertificate(&x509.Certificate{})
			clientCertID2, _ := CertificateManager.Add(clientCertPem2, "")
			defer CertificateManager.Delete(clientCertID2)

			BuildAndLoadAPI(func(spec *APISpec) {
				spec.Domain = "localhost"
				spec.Proxy.ListenPath = "/"
				spec.UseMutualTLSAuth = true
				spec.ClientCertificates = []string{clientCertID2}
			})

			ts.Run(t, test.TestCase{
				Client: client, ErrorMatch: badcertErr, Domain: "localhost",
			})
		})
	})

	t.Run("Multiple APIs on same domain", func(t *testing.T) {
		clientCertID, _ := CertificateManager.Add(clientCertPem, "")
		defer CertificateManager.Delete(clientCertID)

		loadAPIS := func(certs ...string) {
			BuildAndLoadAPI(
				func(spec *APISpec) {
					spec.Proxy.ListenPath = "/with_mutual"
					spec.UseMutualTLSAuth = true
					spec.ClientCertificates = certs
				},
				func(spec *APISpec) {
					spec.Proxy.ListenPath = "/without_mutual"
				},
			)
		}

		t.Run("Without certificate", func(t *testing.T) {
			clientWithoutCert := GetTLSClient(nil, nil)

			loadAPIS()

			certNotMatchErr := "Client TLS certificate is required"
			ts.Run(t, []test.TestCase{
				{
					Path:      "/with_mutual",
					Client:    clientWithoutCert,
					Code:      403,
					BodyMatch: `"error": "` + certNotMatchErr,
				},
				{
					Path:   "/without_mutual",
					Client: clientWithoutCert,
					Code:   200,
				},
			}...)
		})

		t.Run("Client certificate not match", func(t *testing.T) {
			client := GetTLSClient(&clientCert, serverCertPem)

			loadAPIS()

			certNotAllowedErr := `Certificate with SHA256 ` + certs.HexSHA256(clientCert.Certificate[0]) + ` not allowed`

			ts.Run(t, test.TestCase{
				Path:      "/with_mutual",
				Client:    client,
				Code:      403,
				BodyMatch: `"error": "` + certNotAllowedErr,
			})
		})

		t.Run("Client certificate match", func(t *testing.T) {
			loadAPIS(clientCertID)
			client := GetTLSClient(&clientCert, serverCertPem)

			ts.Run(t, test.TestCase{
				Path:   "/with_mutual",
				Client: client,
				Code:   200,
			})
		})
	})
}

func TestUpstreamMutualTLS(t *testing.T) {
	_, _, combinedClientPEM, clientCert := test.GenCertificate(&x509.Certificate{})
	clientCert.Leaf, _ = x509.ParseCertificate(clientCert.Certificate[0])

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))

	// Mutual TLS protected upstream
	pool := x509.NewCertPool()
	upstream.TLS = &tls.Config{
		ClientAuth:         tls.RequireAndVerifyClientCert,
		ClientCAs:          pool,
		InsecureSkipVerify: true,
	}

	upstream.StartTLS()
	defer upstream.Close()

	t.Run("Without API", func(t *testing.T) {
		client := GetTLSClient(&clientCert, nil)

		if _, err := client.Get(upstream.URL); err == nil {
			t.Error("Should reject without certificate")
		}

		pool.AddCert(clientCert.Leaf)

		if _, err := client.Get(upstream.URL); err != nil {
			t.Error("Should pass with valid certificate")
		}
	})

	t.Run("Upstream API", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		ts := StartTest()
		defer ts.Close()

		clientCertID, _ := CertificateManager.Add(combinedClientPEM, "")
		defer CertificateManager.Delete(clientCertID)

		pool.AddCert(clientCert.Leaf)

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
			spec.UpstreamCertificates = map[string]string{
				"*": clientCertID,
			}
		})

		// Should pass with valid upstream certificate
		ts.Run(t, test.TestCase{Code: 200})
	})
}

func TestKeyWithCertificateTLS(t *testing.T) {
	_, _, combinedPEM, _ := test.GenServerCertificate()
	serverCertID, _ := CertificateManager.Add(combinedPEM, "")
	defer CertificateManager.Delete(serverCertID)

	_, _, _, clientCert := test.GenCertificate(&x509.Certificate{})
	clientCertID := certs.HexSHA256(clientCert.Certificate[0])

	globalConf := config.Global()
	globalConf.HttpServerOptions.UseSSL = true
	globalConf.HttpServerOptions.SSLCertificates = []string{serverCertID}
	config.SetGlobal(globalConf)
	defer ResetTestConfig()

	ts := StartTest()
	defer ts.Close()

	BuildAndLoadAPI(func(spec *APISpec) {
		spec.UseKeylessAccess = false
		spec.BaseIdentityProvidedBy = apidef.AuthToken
		spec.Auth.UseCertificate = true
		spec.Proxy.ListenPath = "/"
		spec.OrgID = "default"
	})

	client := GetTLSClient(&clientCert, nil)

	t.Run("Cert unknown", func(t *testing.T) {
		ts.Run(t, test.TestCase{Code: 403, Client: client})
	})

	t.Run("Cert known", func(t *testing.T) {
		_, key := ts.CreateSession(func(s *user.SessionState) {
			s.Certificate = clientCertID
			s.AccessRights = map[string]user.AccessDefinition{"test": {
				APIID: "test", Versions: []string{"v1"},
			}}
		})

		if key == "" {
			t.Fatal("Should create key based on certificate")
		}

		_, key = ts.CreateSession(func(s *user.SessionState) {
			s.Certificate = clientCertID
			s.AccessRights = map[string]user.AccessDefinition{"test": {
				APIID: "test", Versions: []string{"v1"},
			}}
		})

		if key != "" {
			t.Fatal("Should not allow create key based on the same certificate")
		}

		ts.Run(t, test.TestCase{Path: "/", Code: 200, Client: client})
	})
}

func TestAPICertificate(t *testing.T) {
	_, _, combinedPEM, _ := test.GenServerCertificate()
	serverCertID, _ := CertificateManager.Add(combinedPEM, "")
	defer CertificateManager.Delete(serverCertID)

	globalConf := config.Global()
	globalConf.HttpServerOptions.UseSSL = true
	globalConf.HttpServerOptions.SSLCertificates = []string{}
	config.SetGlobal(globalConf)
	defer ResetTestConfig()

	ts := StartTest()
	defer ts.Close()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
	}}}

	t.Run("Cert set via API", func(t *testing.T) {
		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Certificates = []string{serverCertID}
			spec.UseKeylessAccess = true
			spec.Proxy.ListenPath = "/"
		})

		ts.Run(t, test.TestCase{Code: 200, Client: client})
	})

	t.Run("Cert unknown", func(t *testing.T) {
		BuildAndLoadAPI(func(spec *APISpec) {
			spec.UseKeylessAccess = true
			spec.Proxy.ListenPath = "/"
		})

		ts.Run(t, test.TestCase{ErrorMatch: "tls: internal error"})
	})
}

func TestCertificateHandlerTLS(t *testing.T) {
	_, _, combinedServerPEM, serverCert := test.GenServerCertificate()
	serverCertID := certs.HexSHA256(serverCert.Certificate[0])

	clientPEM, _, _, clientCert := test.GenCertificate(&x509.Certificate{})
	clientCertID := certs.HexSHA256(clientCert.Certificate[0])

	ts := StartTest()
	defer ts.Close()

	t.Run("List certificates, empty", func(t *testing.T) {
		ts.Run(t, test.TestCase{
			Path: "/tyk/certs", Code: 200, AdminAuth: true, BodyMatch: `{"certs":null}`,
		})
	})

	t.Run("Should add certificates with and without private keys", func(t *testing.T) {
		ts.Run(t, []test.TestCase{
			// Public Certificate
			{Method: "POST", Path: "/tyk/certs", Data: string(clientPEM), AdminAuth: true, Code: 200, BodyMatch: `"id":"` + clientCertID},
			// Public + Private
			{Method: "POST", Path: "/tyk/certs", Data: string(combinedServerPEM), AdminAuth: true, Code: 200, BodyMatch: `"id":"` + serverCertID},
		}...)
	})

	t.Run("List certificates, non empty", func(t *testing.T) {
		ts.Run(t, []test.TestCase{
			{Method: "GET", Path: "/tyk/certs", AdminAuth: true, Code: 200, BodyMatch: clientCertID},
			{Method: "GET", Path: "/tyk/certs", AdminAuth: true, Code: 200, BodyMatch: serverCertID},
		}...)
	})

	certMetaTemplate := `{"id":"%s","fingerprint":"%s","has_private":%s`

	t.Run("Certificate meta info", func(t *testing.T) {
		clientCertMeta := fmt.Sprintf(certMetaTemplate, clientCertID, clientCertID, "false")
		serverCertMeta := fmt.Sprintf(certMetaTemplate, serverCertID, serverCertID, "true")

		ts.Run(t, []test.TestCase{
			{Method: "GET", Path: "/tyk/certs/" + clientCertID, AdminAuth: true, Code: 200, BodyMatch: clientCertMeta},
			{Method: "GET", Path: "/tyk/certs/" + serverCertID, AdminAuth: true, Code: 200, BodyMatch: serverCertMeta},
			{Method: "GET", Path: "/tyk/certs/" + serverCertID + "," + clientCertID, AdminAuth: true, Code: 200, BodyMatch: "[" + serverCertMeta},
			{Method: "GET", Path: "/tyk/certs/" + serverCertID + "," + clientCertID, AdminAuth: true, Code: 200, BodyMatch: clientCertMeta},
		}...)
	})

	t.Run("Certificate removal", func(t *testing.T) {
		ts.Run(t, []test.TestCase{
			{Method: "DELETE", Path: "/tyk/certs/" + serverCertID, AdminAuth: true, Code: 200},
			{Method: "DELETE", Path: "/tyk/certs/" + clientCertID, AdminAuth: true, Code: 200},
			{Method: "GET", Path: "/tyk/certs", AdminAuth: true, Code: 200, BodyMatch: `{"certs":null}`},
		}...)
	})
}

func TestCipherSuites(t *testing.T) {
	//configure server so we can useSSL and utilize the logic, but skip verification in the clients
	_, _, combinedPEM, _ := test.GenServerCertificate()
	serverCertID, _ := CertificateManager.Add(combinedPEM, "")
	defer CertificateManager.Delete(serverCertID)

	globalConf := config.Global()
	globalConf.HttpServerOptions.UseSSL = true
	globalConf.HttpServerOptions.Ciphers = []string{"TLS_RSA_WITH_RC4_128_SHA", "TLS_RSA_WITH_3DES_EDE_CBC_SHA", "TLS_RSA_WITH_AES_128_CBC_SHA"}
	globalConf.HttpServerOptions.SSLCertificates = []string{serverCertID}
	config.SetGlobal(globalConf)
	defer ResetTestConfig()

	ts := StartTest()
	defer ts.Close()

	BuildAndLoadAPI(func(spec *APISpec) {
		spec.Proxy.ListenPath = "/"
	})

	//matching ciphers
	t.Run("Cipher match", func(t *testing.T) {

		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			CipherSuites:       getCipherAliases([]string{"TLS_RSA_WITH_RC4_128_SHA", "TLS_RSA_WITH_3DES_EDE_CBC_SHA", "TLS_RSA_WITH_AES_128_CBC_SHA"}),
			InsecureSkipVerify: true,
		}}}

		// If there is an internal TLS error it will fail test
		ts.Run(t, test.TestCase{Client: client, Path: "/"})
	})

	t.Run("Cipher non-match", func(t *testing.T) {

		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			CipherSuites:       getCipherAliases([]string{"TLS_RSA_WITH_AES_256_CBC_SHA"}), // not matching ciphers
			InsecureSkipVerify: true,
		}}}

		ts.Run(t, test.TestCase{Client: client, Path: "/", ErrorMatch: "tls: handshake failure"})
	})
}

func TestPublicKeyPinning(t *testing.T) {
	_, _, _, serverCert := test.GenServerCertificate()
	x509Cert, _ := x509.ParseCertificate(serverCert.Certificate[0])
	pubDer, _ := x509.MarshalPKIXPublicKey(x509Cert.PublicKey)
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDer})
	pubID, _ := CertificateManager.Add(pubPem, "")
	defer CertificateManager.Delete(pubID)

	if pubID != certs.HexSHA256(pubDer) {
		t.Error("Certmanager returned wrong pub key fingerprint:", certs.HexSHA256(pubDer), pubID)
	}

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))
	upstream.TLS = &tls.Config{
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{serverCert},
	}

	upstream.StartTLS()
	defer upstream.Close()

	t.Run("Pub key match", func(t *testing.T) {
		globalConf := config.Global()
		// For host using pinning, it should ignore standard verification in all cases, e.g setting variable below does nothing
		globalConf.ProxySSLInsecureSkipVerify = false
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.PinnedPublicKeys = map[string]string{"127.0.0.1": pubID}
			spec.Proxy.TargetURL = upstream.URL
		})

		ts.Run(t, test.TestCase{Code: 200})
	})

	t.Run("Pub key not match", func(t *testing.T) {
		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.PinnedPublicKeys = map[string]string{"127.0.0.1": "wrong"}
			spec.Proxy.TargetURL = upstream.URL
		})

		ts.Run(t, test.TestCase{Code: 500})
	})

	t.Run("Global setting", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.Security.PinnedPublicKeys = map[string]string{"127.0.0.1": "wrong"}
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
		})

		ts.Run(t, test.TestCase{Code: 500})
	})

	t.Run("Though proxy", func(t *testing.T) {
		_, _, _, proxyCert := test.GenServerCertificate()
		proxy := initProxy("https", &tls.Config{
			Certificates: []tls.Certificate{proxyCert},
		})

		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		config.SetGlobal(globalConf)
		defer ResetTestConfig()

		defer proxy.Stop()

		ts := StartTest()
		defer ts.Close()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
			spec.Proxy.Transport.ProxyURL = proxy.URL
			spec.PinnedPublicKeys = map[string]string{"*": "wrong"}
		})

		ts.Run(t, test.TestCase{Code: 500})
	})
}

func TestProxyTransport(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("test"))
	}))
	defer upstream.Close()

	defer ResetTestConfig()

	ts := StartTest()
	defer ts.Close()

	//matching ciphers
	t.Run("Global: Cipher match", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		// force creating new transport on each reque
		globalConf.MaxConnTime = -1

		globalConf.ProxySSLCipherSuites = []string{"TLS_RSA_WITH_AES_128_CBC_SHA"}
		config.SetGlobal(globalConf)
		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
		})
		ts.Run(t, test.TestCase{Path: "/", Code: 200})
	})

	t.Run("Global: Cipher not match", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		// force creating new transport on each reque
		globalConf.MaxConnTime = -1

		globalConf.ProxySSLCipherSuites = []string{"TLS_RSA_WITH_RC4_128_SHA"}
		config.SetGlobal(globalConf)
		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
		})
		ts.Run(t, test.TestCase{Path: "/", Code: 500})
	})

	t.Run("API: Cipher override", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		// force creating new transport on each reque
		globalConf.MaxConnTime = -1

		globalConf.ProxySSLCipherSuites = []string{"TLS_RSA_WITH_RC4_128_SHA"}
		config.SetGlobal(globalConf)
		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
			spec.Proxy.Transport.SSLCipherSuites = []string{"TLS_RSA_WITH_AES_128_CBC_SHA"}
		})

		ts.Run(t, test.TestCase{Path: "/", Code: 200})
	})

	t.Run("API: MinTLS not match", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		// force creating new transport on each reque
		globalConf.MaxConnTime = -1

		globalConf.ProxySSLMinVersion = 772
		config.SetGlobal(globalConf)
		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
			spec.Proxy.Transport.SSLCipherSuites = []string{"TLS_RSA_WITH_AES_128_CBC_SHA"}
		})

		ts.Run(t, test.TestCase{Path: "/", Code: 500})
	})

	t.Run("API: Invalid proxy", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		// force creating new transport on each reque
		globalConf.MaxConnTime = -1

		globalConf.ProxySSLMinVersion = 771
		config.SetGlobal(globalConf)
		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.TargetURL = upstream.URL
			spec.Proxy.Transport.SSLCipherSuites = []string{"TLS_RSA_WITH_AES_128_CBC_SHA"}
			// Invalid proxy
			spec.Proxy.Transport.ProxyURL = upstream.URL
		})

		ts.Run(t, test.TestCase{Path: "/", Code: 500})
	})

	t.Run("API: Valid proxy", func(t *testing.T) {
		globalConf := config.Global()
		globalConf.ProxySSLInsecureSkipVerify = true
		// force creating new transport on each reque
		globalConf.MaxConnTime = -1

		globalConf.ProxySSLMinVersion = 771
		config.SetGlobal(globalConf)

		_, _, _, proxyCert := test.GenServerCertificate()
		proxy := initProxy("https", &tls.Config{
			Certificates: []tls.Certificate{proxyCert},
		})
		defer proxy.Stop()

		BuildAndLoadAPI(func(spec *APISpec) {
			spec.Proxy.ListenPath = "/"
			spec.Proxy.Transport.SSLCipherSuites = []string{"TLS_RSA_WITH_AES_128_CBC_SHA"}
			spec.Proxy.Transport.ProxyURL = proxy.URL
		})

		client := getTLSClient(nil, nil)
		client.Transport = &http.Transport{
			TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		}
		ts.Run(t, test.TestCase{Path: "/", Code: 200, Client: client})
	})
}
