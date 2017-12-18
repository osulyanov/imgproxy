package main

import (
	"compress/gzip"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var mimes = map[imageType]string{
	JPEG: "image/jpeg",
	PNG:  "image/png",
	WEBP: "image/webp",
}

type httpHandler struct {
	sem chan struct{}
}

func newHTTPHandler() *httpHandler {
	return &httpHandler{make(chan struct{}, conf.Concurrency)}
}

func parsePath(r *http.Request) (string, processingOptions, error) {
	var po processingOptions
	var err error

	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")

	if len(parts) < 3 {
		return "", po, errors.New("Invalid path")
	}

	publicToken := parts[0]

	if err = validatePath(publicToken, strings.TrimPrefix(path, fmt.Sprintf("/%s", publicToken))); err != nil {
		return "", po, err
	}

	filenameParts := strings.Split(strings.Join(parts[2:], ""), ".")

	if len(filenameParts) < 2 {
		po.format = imageTypes["jpg"]
	} else if f, ok := imageTypes[filenameParts[1]]; ok {
		po.format = f
	} else {
		return "", po, fmt.Errorf("Invalid image format: %s", filenameParts[1])
	}

	if !vipsTypeSupportSave[po.format] {
		return "", po, errors.New("Resulting image type not supported")
	}

	tile_name, err := base64.RawURLEncoding.DecodeString(filenameParts[0])
	if err != nil {
		return "", po, errors.New("Invalid filename encoding")
	}


	path_parts := strings.Split(strings.TrimPrefix(string(tile_name), "/"), "/")

	tile_path := strings.Join(path_parts[4:], "/")

	domain_signed :=  strings.Join(path_parts[:4], "")
	domain, err := base64.RawURLEncoding.DecodeString(domain_signed)

	filename := strings.Join( []string{string(domain), string(tile_path)}, "/")

	return string(filename), po, nil
}

func logResponse(status int, msg string) {
	var color int

	if status >= 500 {
		color = 31
	} else if status >= 400 {
		color = 33
	} else {
		color = 32
	}

	log.Printf("|\033[7;%dm %d \033[0m| %s\n", color, status, msg)
}

func respondWithImage(r *http.Request, rw http.ResponseWriter, data []byte, imgURL string, po processingOptions, duration time.Duration) {
	gzipped := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && conf.GZipCompression > 0

	rw.Header().Set("Expires", time.Now().Add(time.Second*time.Duration(conf.TTL)).Format(http.TimeFormat))
	rw.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d, public", conf.TTL))
	rw.Header().Set("Content-Type", mimes[po.format])
	if gzipped {
		rw.Header().Set("Content-Encoding", "gzip")
	}

	rw.WriteHeader(200)

	if gzipped {
		gz, _ := gzip.NewWriterLevel(rw, conf.GZipCompression)
		gz.Write(data)
		gz.Close()
	} else {
		rw.Write(data)
	}

	logResponse(200, fmt.Sprintf("Processed in %s: %s; %+v", duration, imgURL, po))
}

func respondWithError(rw http.ResponseWriter, err imgproxyError) {
	logResponse(err.StatusCode, err.Message)

	rw.WriteHeader(err.StatusCode)
	rw.Write([]byte(err.PublicMessage))
}

func checkSecret(s string) bool {
	if len(conf.Secret) == 0 {
		return true
	}
	return strings.HasPrefix(s, "Bearer ") && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(s, "Bearer ")), []byte(conf.Secret)) == 1
}

func (h *httpHandler) lock(t *timer) {
	select {
	case h.sem <- struct{}{}:
		// Go ahead
	case <-t.Timer:
		panic(t.TimeoutErr())
	}
}

func (h *httpHandler) unlock() {
	<-h.sem
}

func (h *httpHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	log.Printf("GET: %s\n", r.URL.RequestURI())

	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(imgproxyError); ok {
				respondWithError(rw, err)
			} else {
				respondWithError(rw, newUnexpectedError(r.(error), 4))
			}
		}
	}()

	t := startTimer(time.Duration(conf.WriteTimeout) * time.Second)

	h.lock(t)
	defer h.unlock()

	if !checkSecret(r.Header.Get("Authorization")) {
		panic(invalidSecretErr)
	}

	if r.URL.Path == "/health" {
		rw.WriteHeader(200);
		rw.Write([]byte("imgproxy is running"));
		return
	}

	imgURL, procOpt, err := parsePath(r)
	if err != nil {
		panic(newError(404, err.Error(), "Invalid image url"))
	}

	if _, err = url.ParseRequestURI(imgURL); err != nil {
		panic(newError(404, err.Error(), "Invalid image url"))
	}

	b, _, err := downloadImage(imgURL)
	if err != nil {
		panic(newError(404, err.Error(), "Image is unreachable"))
	}

	t.Check()

	respondWithImage(r, rw, b, imgURL, procOpt, t.Since())
}
