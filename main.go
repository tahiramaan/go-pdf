package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	wkhtml "github.com/SebastiaanKlippert/go-wkhtmltopdf"
)

const (
	tempDir  = "./temp"
	lifeTime = 5 * time.Minute
)

type Request struct {
	HTML   string `json:"html"`
	Prefix string `json:"prefix"`
}

type SuccessResponse struct {
	Success     bool      `json:"success"`
	Link        string    `json:"link"`
	ExpiresAt   time.Time `json:"expires_at"`
	TimeElapsed int64     `json:"time_elapsed"` // ms
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

func main() {
	os.MkdirAll(tempDir, 0755)

	http.HandleFunc("/convert", cors(convertHandler))
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(tempDir))))

	log.Println("PDF API running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func convertHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		writeError(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	body, _ := io.ReadAll(r.Body)

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, "invalid json body", http.StatusBadRequest)
		return
	}

	if req.HTML == "" {
		writeError(w, "html is required", http.StatusBadRequest)
		return
	}

	prefix := req.Prefix
	if prefix == "" {
		prefix = "file"
	}

	filename := buildFileName(prefix)
	fullPath := filepath.Join(tempDir, filename)

	if err := htmlToPDF(req.HTML, fullPath); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	expiry := time.Now().Add(lifeTime)
	go deleteLater(fullPath)

	server := getServerURL(r)

	resp := SuccessResponse{
		Success:     true,
		Link:        fmt.Sprintf("%s/files/%s", server, filename),
		ExpiresAt:   expiry.UTC(),
		TimeElapsed: time.Since(start).Milliseconds(),
	}

	json.NewEncoder(w).Encode(resp)
}

func htmlToPDF(html string, output string) error {
	pdfg, err := wkhtml.NewPDFGenerator()
	if err != nil {
		return err
	}

	page := wkhtml.NewPageReader(io.NopCloser(&stringReader{html}))
	page.EnableLocalFileAccess.Set(true)

	pdfg.AddPage(page)

	// 0.25 inch ≈ 6mm
	pdfg.MarginTop.Set(6)
	pdfg.MarginBottom.Set(6)
	pdfg.MarginLeft.Set(6)
	pdfg.MarginRight.Set(6)

	pdfg.PageSize.Set(wkhtml.PageSizeA4)

	if err := pdfg.Create(); err != nil {
		return err
	}

	return pdfg.WriteFile(output)
}

func writeError(w http.ResponseWriter, msg string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Success: false,
		Error:   msg,
	})
}

type stringReader struct {
	s string
}

func (sr *stringReader) Read(p []byte) (int, error) {
	if len(sr.s) == 0 {
		return 0, io.EOF
	}
	n := copy(p, sr.s)
	sr.s = sr.s[n:]
	return n, nil
}

func buildFileName(prefix string) string {
	ts := time.Now().Format("20060102_150405")
	hashSource := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())

	h := sha1.Sum([]byte(hashSource))
	hash := hex.EncodeToString(h[:])[:10]

	return fmt.Sprintf("%s_%s_%s.pdf", prefix, ts, hash)
}

func getServerURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func deleteLater(path string) {
	time.Sleep(lifeTime)
	os.Remove(path)
}

func cors(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

		if r.Method == http.MethodOptions {
			return
		}
		h(w, r)
	}
}
