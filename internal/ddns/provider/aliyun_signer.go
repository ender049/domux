package ddnsprovider

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"net/url"
	"strconv"
	"time"
)

// Adapted from ddns-go util/aliyun_signer.go and util/aliyun_signer_util.go.

var signerMethods = map[string]func() hash.Hash{
	"HMAC-SHA1":   sha1.New,
	"HMAC-SHA256": sha256.New,
	"HMAC-MD5":    md5.New,
}

func aliyunSigner(accessKeyID, accessSecret string, params *url.Values, httpMethod string, apiVersion string) {
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureNonce", strconv.FormatInt(time.Now().UnixNano(), 10))
	params.Set("AccessKeyId", accessKeyID)
	params.Set("SignatureVersion", "1.0")
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("Format", "JSON")
	params.Set("Version", apiVersion)
	params.Set("Signature", hmacSignToB64("HMAC-SHA1", httpMethod, accessSecret, *params))
}

func hmacSignToB64(signMethod string, httpMethod string, appKeySecret string, vals url.Values) string {
	return base64.StdEncoding.EncodeToString(hmacSign(signMethod, httpMethod, appKeySecret, vals))
}

func hmacSign(signMethod string, httpMethod string, appKeySecret string, vals url.Values) []byte {
	key := []byte(appKeySecret + "&")
	method := signerMethods[signMethod]
	if method == nil {
		method = sha1.New
	}
	h := hmac.New(method, key)
	makeDataToSign(h, httpMethod, vals)
	return h.Sum(nil)
}

type signerToken struct {
	s string
	e bool
}

func makeDataToSign(w io.Writer, httpMethod string, vals url.Values) {
	in := make(chan *signerToken)
	go func() {
		in <- &signerToken{s: httpMethod}
		in <- &signerToken{s: "&"}
		in <- &signerToken{s: "/", e: true}
		in <- &signerToken{s: "&"}
		in <- &signerToken{s: vals.Encode(), e: true}
		close(in)
	}()
	specialURLEncode(in, w)
}

func specialURLEncode(in <-chan *signerToken, w io.Writer) {
	for token := range in {
		if !token.e {
			_, _ = io.WriteString(w, token.s)
			continue
		}
		for i := 0; i < len(token.s); i++ {
			ch := token.s[i]
			switch ch {
			case '%':
				if i+2 < len(token.s) && token.s[i:i+3] == "%7E" {
					_, _ = w.Write([]byte("~"))
					i += 2
					continue
				}
				fallthrough
			case '*', '/', '&', '=':
				_, _ = fmt.Fprintf(w, "%%%02X", ch)
			case '+':
				_, _ = w.Write([]byte("%20"))
			default:
				_, _ = fmt.Fprintf(w, "%c", ch)
			}
		}
	}
}
