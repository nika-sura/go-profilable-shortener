package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bbrodriges/practicum-shortener/models"
)

func TestMain(m *testing.M) {
	go func() {
		err := run()
		if err != nil {
			panic(err)
		}
	}()
	os.Exit(m.Run())
}

func Test_run(t *testing.T) {
	time.Sleep(200 * time.Millisecond)

	targetURL := "https://praktikum.yandex.ru/"

	for i := 0; i < 50; i++ {
		expectedID := fmt.Sprintf("%x", i)

		t.Run("shorten", func(t *testing.T) {
			expectResponse := "http://localhost:8080/" + expectedID
			var actualResponse string

			{
				body := bytes.NewBufferString(targetURL)
				r := httptest.NewRequest("POST", "http://localhost:8080/", body)
				r.RequestURI = ""

				resp, err := http.DefaultClient.Do(r)
				require.NoError(t, err)
				require.Equal(t, http.StatusCreated, resp.StatusCode)

				defer resp.Body.Close()

				b, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				actualResponse = string(b)

				require.Equal(t, expectResponse, actualResponse)
			}

			{
				r := httptest.NewRequest("GET", actualResponse, nil)
				r.RequestURI = ""

				resp, err := http.DefaultTransport.RoundTrip(r)
				require.NoError(t, err)

				defer resp.Body.Close()

				assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
				assert.Equal(t, targetURL, resp.Header.Get("Location"))
			}
		})
	}

	for i := 50; i < 100; i++ {
		expectedID := fmt.Sprintf("%x", i)

		t.Run("shortenAPI", func(t *testing.T) {
			expectResponse := "{\"result\":\"http://localhost:8080/" + expectedID + "\"}\n"
			var actualResponse string

			{
				body := bytes.NewBufferString(`{"url":"` + targetURL + `"}`)
				r := httptest.NewRequest("POST", "http://localhost:8080/api/shorten", body)
				r.RequestURI = ""

				resp, err := http.DefaultClient.Do(r)
				require.NoError(t, err)
				require.Equal(t, http.StatusCreated, resp.StatusCode)

				defer resp.Body.Close()

				b, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				actualResponse = string(b)

				require.Equal(t, expectResponse, actualResponse)
			}

			{
				var target models.ShortenResponse
				err := json.Unmarshal([]byte(actualResponse), &target)
				require.NoError(t, err)

				r := httptest.NewRequest("GET", target.Result, nil)
				r.RequestURI = ""

				resp, err := http.DefaultTransport.RoundTrip(r)
				require.NoError(t, err)

				defer resp.Body.Close()

				assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
				assert.Equal(t, targetURL, resp.Header.Get("Location"))
			}
		})
	}
}

func TestEndToEnd(t *testing.T) {
	originalURL := "https://praktikum.yandex.ru"

	// create HTTP client without redirects support
	errRedirectBlocked := errors.New("HTTP redirect blocked")
	redirPolicy := resty.RedirectPolicyFunc(func(_ *http.Request, _ []*http.Request) error {
		return errRedirectBlocked
	})

	httpc := resty.New().
		SetHostURL("http://localhost:8080").
		SetRedirectPolicy(redirPolicy)

	// shorten URL
	req := httpc.R().
		SetBody(originalURL)
	resp, err := req.Post("/")
	require.NoError(t, err)

	shortenURL := string(resp.Body())

	assert.Equal(t, http.StatusCreated, resp.StatusCode())
	assert.NoError(t, func() error {
		_, err := url.Parse(shortenURL)
		return err
	}())

	// expand URL
	req = resty.New().
		SetRedirectPolicy(redirPolicy).
		R()
	resp, err = req.Get(shortenURL)
	if !errors.Is(err, errRedirectBlocked) {
		require.NoError(t, err)
	}

	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode())
	assert.Equal(t, originalURL, resp.Header().Get("Location"))
}
