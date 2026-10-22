package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxWisherRunes = 20
	maxWishRunes   = 30
	maxBodyBytes   = 4 << 10
)

type wishRequest struct {
	Wisher string `json:"wisher"`
	Wish   string `json:"wish"`
}

type apiResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func newRouter(store *wishStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/wish", handleWish(store))
	mux.HandleFunc("GET /api/health", handleHealth(store))

	return withCORS(withLogging(mux))
}

func handleWish(store *wishStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "请使用 application/json 格式提交")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer r.Body.Close()

		var req wishRequest
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "请求内容不是有效的 JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "请求体只能包含一个 JSON 对象")
			return
		}

		req.Wisher = strings.TrimSpace(req.Wisher)
		req.Wish = strings.TrimSpace(req.Wish)
		if message, ok := validateWish(req); !ok {
			writeError(w, http.StatusBadRequest, message)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		saved, err := store.Create(ctx, req.Wisher, req.Wish, time.Now())
		if err != nil {
			log.Printf("保存愿望失败: %v", err)
			writeError(w, http.StatusInternalServerError, "许愿保存失败，请稍后重试")
			return
		}

		writeJSON(w, http.StatusCreated, apiResponse{
			Success: true,
			Message: "许愿成功",
			Data:    saved,
		})
	}
}

func handleHealth(store *wishStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := store.Ping(ctx); err != nil {
			log.Printf("数据库健康检查失败: %v", err)
			writeError(w, http.StatusServiceUnavailable, "数据库连接异常")
			return
		}

		writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "服务正常"})
	}
}

func validateWish(req wishRequest) (string, bool) {
	switch {
	case req.Wisher == "" && req.Wish == "":
		return "许愿人和愿望不能为空", false
	case req.Wisher == "":
		return "许愿人不能为空", false
	case req.Wish == "":
		return "愿望不能为空", false
	case utf8.RuneCountInString(req.Wisher) > maxWisherRunes:
		return fmt.Sprintf("许愿人不能超过 %d 个字符", maxWisherRunes), false
	case utf8.RuneCountInString(req.Wish) > maxWishRunes:
		return fmt.Sprintf("愿望不能超过 %d 个字符", maxWishRunes), false
	}

	return "", true
}

func writeJSON(w http.ResponseWriter, status int, payload apiResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("写入响应失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiResponse{Success: false, Message: message})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, status, time.Since(startedAt).Round(time.Millisecond))
	})
}
