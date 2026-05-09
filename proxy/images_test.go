package proxy

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex2api/auth"
	"github.com/codex2api/database"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="

func tinyPNGByteSize(t *testing.T) int {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatalf("decode tiny png fixture: %v", err)
	}
	return len(data)
}

func TestBuildImagesResponsesRequestMatchesReferenceChain(t *testing.T) {
	tool := []byte(`{"type":"image_generation","action":"generate","model":"gpt-image-2","size":"1024x1024"}`)

	body := buildImagesResponsesRequest("draw a cat", nil, tool)

	if got := gjson.GetBytes(body, "model").String(); got != defaultImagesMainModel {
		t.Fatalf("responses model = %q, want %q", got, defaultImagesMainModel)
	}
	if got := gjson.GetBytes(body, "tool_choice.type").String(); got != "image_generation" {
		t.Fatalf("tool_choice.type = %q, want image_generation", got)
	}
	if got := gjson.GetBytes(body, "tools.0.type").String(); got != "image_generation" {
		t.Fatalf("tools.0.type = %q, want image_generation", got)
	}
	if got := gjson.GetBytes(body, "tools.0.model").String(); got != "gpt-image-2" {
		t.Fatalf("tools.0.model = %q, want gpt-image-2", got)
	}
	if got := gjson.GetBytes(body, "input.0.content.0.text").String(); got != "draw a cat" {
		t.Fatalf("prompt = %q, want draw a cat", got)
	}
}

func TestNormalizeImageDataURLRewritesOctetStreamImage(t *testing.T) {
	raw := "data:application/octet-stream;base64," + tinyPNGBase64

	normalized, ok := normalizeImageDataURL(raw)
	if !ok {
		t.Fatal("normalizeImageDataURL returned ok=false")
	}
	if !strings.HasPrefix(normalized, "data:image/png;base64,") {
		t.Fatalf("normalized = %q, want image/png data URL", normalized[:min(len(normalized), 40)])
	}
}

func TestNextImageAccountPrefersPlusOrHigherPlan(t *testing.T) {
	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{DBID: 1, AccessToken: "free-token", PlanType: "free"})
	store.AddAccount(&auth.Account{DBID: 2, AccessToken: "plus-token", PlanType: "plus"})
	handler := &Handler{store: store}

	account, _ := handler.nextImageAccount(0, 0, nil)
	if account == nil {
		t.Fatal("nextImageAccount returned nil")
	}
	defer store.Release(account)

	if account.DBID != 2 {
		t.Fatalf("nextImageAccount picked account %d, want plus account 2", account.DBID)
	}
}

func TestNextImageAccountFallsBackToFreeWhenNoPaidAccountAvailable(t *testing.T) {
	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{DBID: 1, AccessToken: "free-token", PlanType: "free"})
	handler := &Handler{store: store}

	account, _ := handler.nextImageAccount(0, 0, nil)
	if account == nil {
		t.Fatal("nextImageAccount returned nil")
	}
	defer store.Release(account)

	if account.DBID != 1 {
		t.Fatalf("nextImageAccount picked account %d, want fallback free account 1", account.DBID)
	}
}

func TestNextImageAccountHonorsTargetAccount(t *testing.T) {
	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{DBID: 1, AccessToken: "free-token", PlanType: "free"})
	store.AddAccount(&auth.Account{DBID: 2, AccessToken: "plus-token", PlanType: "plus"})
	handler := &Handler{store: store}

	account, _ := handler.nextImageAccount(0, 1, nil)
	if account == nil {
		t.Fatal("nextImageAccount returned nil")
	}
	defer store.Release(account)

	if account.DBID != 1 {
		t.Fatalf("nextImageAccount picked account %d, want forced account 1", account.DBID)
	}
}

func TestNextImageAccountDoesNotFallbackWhenTargetUnavailable(t *testing.T) {
	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{DBID: 2, AccessToken: "plus-token", PlanType: "plus"})
	handler := &Handler{store: store}

	account, _ := handler.nextImageAccount(0, 1, nil)
	if account != nil {
		defer store.Release(account)
		t.Fatalf("nextImageAccount picked account %d, want nil for unavailable target", account.DBID)
	}
}

func TestImagesGenerationsUsesGenericProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotPath string
	var gotAuth string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"model":"gpt-image-2","data":[{"b64_json":"` + tinyPNGBase64 + `"}]}`))
	}))
	defer upstream.Close()

	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{
		DBID:         9,
		Type:         "api_key",
		BaseURL:      upstream.URL,
		APIKey:       "upstream-key",
		ProviderName: "generic",
	})
	handler := &Handler{store: store}
	router := gin.New()
	router.POST("/v1/images/generations", handler.ImagesGenerations)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{
		"model":"gpt-image-2-4k",
		"prompt":"draw a cat",
		"style":"cinematic",
		"response_format":"b64_json"
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/v1/images/generations" {
		t.Fatalf("upstream path = %q, want /v1/images/generations", gotPath)
	}
	if gotAuth != "Bearer upstream-key" {
		t.Fatalf("Authorization = %q, want Bearer upstream-key", gotAuth)
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "gpt-image-2" {
		t.Fatalf("generic body model = %q, want gpt-image-2; body=%s", got, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "size").String(); got != defaultImages4KSize {
		t.Fatalf("generic body size = %q, want %s; body=%s", got, defaultImages4KSize, string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "prompt").String(); !strings.Contains(got, "Style guidance: cinematic") {
		t.Fatalf("generic body prompt = %q, want style guidance", got)
	}
	if gjson.GetBytes(gotBody, "style").Exists() {
		t.Fatalf("generic body should not include style after folding prompt: %s", string(gotBody))
	}
	if got := gjson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").String(); got != tinyPNGBase64 {
		t.Fatalf("response b64 = %q, want fixture", got)
	}
}

func TestImagesEditsMultipartUsesGenericProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotPath string
	var gotAuth string
	var gotContentType string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"model":"gpt-image-2","data":[{"b64_json":"` + tinyPNGBase64 + `"}]}`))
	}))
	defer upstream.Close()

	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{
		DBID:         9,
		Type:         "api_key",
		BaseURL:      upstream.URL,
		APIKey:       "upstream-key",
		ProviderName: "generic",
	})
	handler := &Handler{store: store}
	router := gin.New()
	router.POST("/v1/images/edits", handler.ImagesEdits)

	imageBytes, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "gpt-image-2-2k")
	_ = writer.WriteField("prompt", "edit a cat")
	_ = writer.WriteField("style", "flat poster")
	part, err := writer.CreateFormFile("image", "input.png")
	if err != nil {
		t.Fatalf("create image field: %v", err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		t.Fatalf("write image field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/v1/images/edits" {
		t.Fatalf("upstream path = %q, want /v1/images/edits", gotPath)
	}
	if gotAuth != "Bearer upstream-key" {
		t.Fatalf("Authorization = %q, want Bearer upstream-key", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data;") {
		t.Fatalf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	upstreamBody := string(gotBody)
	if !strings.Contains(upstreamBody, `name="prompt"`) || !strings.Contains(upstreamBody, "Style guidance: flat poster") {
		t.Fatalf("generic multipart prompt missing style guidance: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `name="model"`) || !strings.Contains(upstreamBody, "gpt-image-2") {
		t.Fatalf("generic multipart model missing normalized value: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `name="size"`) || !strings.Contains(upstreamBody, defaultImages2KPortraitSize) {
		t.Fatalf("generic multipart size missing default 2K value: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `name="image"; filename="input.png"`) {
		t.Fatalf("generic multipart image file missing: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, "Content-Type: image/png") {
		t.Fatalf("generic multipart image content type missing: %s", upstreamBody)
	}
}

func TestImagesEditsJSONUsesGenericProviderMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotPath string
	var gotAuth string
	var gotContentType string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"model":"gpt-image-2","data":[{"b64_json":"` + tinyPNGBase64 + `"}]}`))
	}))
	defer upstream.Close()

	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{
		DBID:         9,
		Type:         "api_key",
		BaseURL:      upstream.URL,
		APIKey:       "upstream-key",
		ProviderName: "generic",
	})
	handler := &Handler{store: store}
	router := gin.New()
	router.POST("/v1/images/edits", handler.ImagesEdits)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(`{
		"model":"gpt-image-2-4k",
		"prompt":"edit square icon",
		"images":[{"image_url":"data:image/png;base64,`+tinyPNGBase64+`"}],
		"mask":{"image_url":"data:image/png;base64,`+tinyPNGBase64+`"},
		"response_format":"b64_json"
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/v1/images/edits" {
		t.Fatalf("upstream path = %q, want /v1/images/edits", gotPath)
	}
	if gotAuth != "Bearer upstream-key" {
		t.Fatalf("Authorization = %q, want Bearer upstream-key", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data;") {
		t.Fatalf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	upstreamBody := string(gotBody)
	if !strings.Contains(upstreamBody, `name="model"`) || !strings.Contains(upstreamBody, "gpt-image-2") {
		t.Fatalf("generic JSON edit multipart model missing normalized value: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `name="size"`) || !strings.Contains(upstreamBody, defaultImages4KSquareSize) {
		t.Fatalf("generic JSON edit multipart size missing default 4K square value: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `name="image"; filename="image-1.png"`) {
		t.Fatalf("generic JSON edit multipart image file missing: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `name="mask"; filename="mask.png"`) {
		t.Fatalf("generic JSON edit multipart mask file missing: %s", upstreamBody)
	}
	if got := strings.Count(upstreamBody, "Content-Type: image/png"); got < 2 {
		t.Fatalf("generic JSON edit multipart image content types = %d, want at least 2: %s", got, upstreamBody)
	}
}

func TestAppendImageStyleToPrompt(t *testing.T) {
	got := AppendImageStyleToPrompt("draw a cat", "cinematic sticker")
	if !strings.Contains(got, "draw a cat") || !strings.Contains(got, "Style guidance: cinematic sticker") {
		t.Fatalf("styled prompt = %q", got)
	}
	if got := AppendImageStyleToPrompt("draw a cat", " "); got != "draw a cat" {
		t.Fatalf("unstyled prompt = %q, want draw a cat", got)
	}
}

func TestNormalizeImageToolModelAliases(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		want     string
		wantSize string
	}{
		{name: "default model", model: "gpt-image-2", want: "gpt-image-2", wantSize: defaultImages1KSize},
		{name: "2k alias", model: "gpt-image-2-2k", want: "gpt-image-2", wantSize: defaultImages2KSize},
		{name: "4k alias", model: "gpt-image-2-4k", want: "gpt-image-2", wantSize: defaultImages4KSize},
		{name: "other image model", model: "gpt-image-1.5", want: "gpt-image-1.5", wantSize: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotSize := normalizeImageToolModel(test.model)
			if got != test.want || gotSize != test.wantSize {
				t.Fatalf("normalizeImageToolModel(%q) = (%q, %q), want (%q, %q)", test.model, got, gotSize, test.want, test.wantSize)
			}
		})
	}
}

func TestNormalizeImageToolModelForPromptInfersAspect(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		prompt   string
		want     string
		wantSize string
	}{
		{
			name:     "1k landscape prompt",
			model:    defaultImagesToolModel,
			prompt:   "desktop wallpaper, wide cinematic city",
			want:     defaultImagesToolModel,
			wantSize: defaultImages1KLandscapeSize,
		},
		{
			name:     "2k portrait prompt",
			model:    imageModel2KAlias,
			prompt:   "mobile wallpaper portrait neon cat",
			want:     defaultImagesToolModel,
			wantSize: defaultImages2KPortraitSize,
		},
		{
			name:     "4k square prompt",
			model:    imageModel4KAlias,
			prompt:   "square app icon logo",
			want:     defaultImagesToolModel,
			wantSize: defaultImages4KSquareSize,
		},
		{
			name:     "4k no prompt keeps default",
			model:    imageModel4KAlias,
			prompt:   "a detailed fantasy city",
			want:     defaultImagesToolModel,
			wantSize: defaultImages4KSize,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotSize := normalizeImageToolModelForPrompt(test.model, test.prompt)
			if got != test.want || gotSize != test.wantSize {
				t.Fatalf("normalizeImageToolModelForPrompt(%q, %q) = (%q, %q), want (%q, %q)", test.model, test.prompt, got, gotSize, test.want, test.wantSize)
			}
		})
	}
}

func TestSetDefaultImageToolSizePreservesExplicitSize(t *testing.T) {
	tool := []byte(`{"type":"image_generation","model":"gpt-image-2","size":"1536x1024"}`)

	got := setDefaultImageToolSize(tool, defaultImages4KSize)

	if size := gjson.GetBytes(got, "size").String(); size != "1536x1024" {
		t.Fatalf("size = %q, want explicit size", size)
	}
}

func TestValidateGPTImage2Size(t *testing.T) {
	tests := []struct {
		name    string
		size    string
		wantErr bool
	}{
		{name: "auto", size: "auto"},
		{name: "1k", size: defaultImages1KSize},
		{name: "2k square", size: defaultImages2KSize},
		{name: "4k landscape", size: defaultImages4KSize},
		{name: "4k portrait", size: "2160x3840"},
		{name: "too many pixels", size: "5000x5000", wantErr: true},
		{name: "too wide", size: "4096x1024", wantErr: true},
		{name: "not multiple of 16", size: "1025x1024", wantErr: true},
		{name: "bad format", size: "1024*1024", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateGPTImage2Size(test.size)
			if test.wantErr && err == nil {
				t.Fatalf("validateGPTImage2Size(%q) expected error", test.size)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateGPTImage2Size(%q) unexpected error: %v", test.size, err)
			}
		})
	}
}

func TestValidateResponsesImageGenerationSizes(t *testing.T) {
	valid := []byte(`{"tools":[{"type":"image_generation","model":"gpt-image-2","size":"3840x2160"}]}`)
	if err := validateResponsesImageGenerationSizes(valid); err != nil {
		t.Fatalf("valid image_generation size returned error: %v", err)
	}

	invalid := []byte(`{"tools":[{"type":"image_generation","model":"gpt-image-2","size":"5000x5000"}]}`)
	if err := validateResponsesImageGenerationSizes(invalid); err == nil {
		t.Fatal("expected invalid image_generation size error")
	}

	nonString := []byte(`{"tools":[{"type":"image_generation","model":"gpt-image-2","size":1024}]}`)
	if err := validateResponsesImageGenerationSizes(nonString); err == nil {
		t.Fatal("expected non-string image_generation size error")
	}

	otherModel := []byte(`{"tools":[{"type":"image_generation","model":"gpt-image-1.5","size":"5000x5000"}]}`)
	if err := validateResponsesImageGenerationSizes(otherModel); err != nil {
		t.Fatalf("non gpt-image-2 size should be ignored, got %v", err)
	}
}

func TestBuildImagesResponsesRequestIncludesEditImages(t *testing.T) {
	tool := []byte(`{"type":"image_generation","action":"edit","model":"gpt-image-2"}`)

	body := buildImagesResponsesRequest("replace background", []string{"https://example.com/source.png"}, tool)

	if got := gjson.GetBytes(body, "tools.0.action").String(); got != "edit" {
		t.Fatalf("tools.0.action = %q, want edit", got)
	}
	if got := gjson.GetBytes(body, "input.0.content.1.type").String(); got != "input_image" {
		t.Fatalf("input image type = %q, want input_image", got)
	}
	if got := gjson.GetBytes(body, "input.0.content.1.image_url").String(); got != "https://example.com/source.png" {
		t.Fatalf("input image URL = %q", got)
	}
}

func TestCollectImagesResponseBuildsOpenAIImagePayload(t *testing.T) {
	upstream := `data: {"type":"response.completed","response":{"created_at":1710000000,"usage":{"input_tokens":5,"output_tokens":9},"tool_usage":{"image_gen":{"images":1,"input_tokens":34,"output_tokens":1756}},"tools":[{"type":"image_generation","model":"gpt-image-2","output_format":"png","quality":"high","size":"1024x1024"}],"output":[{"type":"image_generation_call","result":"` + tinyPNGBase64 + `","revised_prompt":"draw a cat","output_format":"png"}]}}` + "\n\n"

	out, usage, imageCount, imageLogInfo, err := collectImagesResponse(strings.NewReader(upstream), "b64_json", "gpt-image-2")
	if err != nil {
		t.Fatalf("collectImagesResponse returned error: %v", err)
	}
	if imageCount != 1 {
		t.Fatalf("imageCount = %d, want 1", imageCount)
	}
	if imageLogInfo.Count != 1 || imageLogInfo.Width != 1 || imageLogInfo.Height != 1 || imageLogInfo.Bytes != tinyPNGByteSize(t) {
		t.Fatalf("imageLogInfo = %#v, want count=1 size=1x1 bytes=%d", imageLogInfo, tinyPNGByteSize(t))
	}
	if usage == nil || usage.InputTokens != 34 || usage.OutputTokens != 1756 {
		t.Fatalf("usage = %#v, want image usage input=34 output=1756", usage)
	}
	if got := gjson.GetBytes(out, "data.0.b64_json").String(); got != tinyPNGBase64 {
		t.Fatalf("b64_json = %q, want tiny PNG", got)
	}
	if got := gjson.GetBytes(out, "data.0.bytes").Int(); got != int64(tinyPNGByteSize(t)) {
		t.Fatalf("bytes = %d, want %d", got, tinyPNGByteSize(t))
	}
	if got := gjson.GetBytes(out, "data.0.width").Int(); got != 1 {
		t.Fatalf("width = %d, want 1", got)
	}
	if got := gjson.GetBytes(out, "data.0.height").Int(); got != 1 {
		t.Fatalf("height = %d, want 1", got)
	}
	if got := gjson.GetBytes(out, "model").String(); got != "gpt-image-2" {
		t.Fatalf("model = %q, want gpt-image-2", got)
	}
	if got := gjson.GetBytes(out, "usage.images").Int(); got != 1 {
		t.Fatalf("usage.images = %d, want 1", got)
	}
}

func TestCollectImagesResponseUsesUpstreamFailureMessage(t *testing.T) {
	upstream := `data: {"type":"response.failed","response":{"error":{"code":"server_error","message":"An error occurred while processing your request. Please include the request ID req-123."}}}` + "\n\n"

	_, _, _, _, err := collectImagesResponse(strings.NewReader(upstream), "b64_json", "gpt-image-2")
	if err == nil {
		t.Fatal("collectImagesResponse returned nil error")
	}
	if got := err.Error(); !strings.Contains(got, "server_error") || !strings.Contains(got, "req-123") {
		t.Fatalf("error = %q, want upstream code and request id", got)
	}
}
