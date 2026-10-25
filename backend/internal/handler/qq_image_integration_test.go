package handler

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type qqHTTPIntegrationGenerator struct{ calls int }

func (g *qqHTTPIntegrationGenerator) Generate(context.Context, qqimage.GenerationRequest) (qqimage.Artifact, error) {
	g.calls++
	var b bytes.Buffer
	err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	sum := sha256.Sum256(b.Bytes())
	return qqimage.Artifact{Data: b.Bytes(), MIME: "image/png", Width: 2, Height: 2, SHA256: hex.EncodeToString(sum[:])}, err
}

type qqHTTPIntegrationSender struct{ sent int }

func (s *qqHTTPIntegrationSender) SendText(context.Context, *qqimage.RuntimeConfig, *qqimage.QQEvent, string, int) qqimage.QQSendResult {
	return qqimage.QQSendResult{Status: "sent"}
}
func (s *qqHTTPIntegrationSender) SendDelivery(context.Context, *qqimage.RuntimeConfig, *qqimage.Delivery, string) qqimage.QQSendResult {
	s.sent++
	return qqimage.QQSendResult{Status: "sent"}
}

func TestQQImageHTTPPostgresSignedBindingGenerationAndPrivateRetrieval(t *testing.T) {
	dsn := os.Getenv("QQ_IMAGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_IMAGE_TEST_DATABASE_URL required for local PostgreSQL integration")
	}
	require.True(t, strings.Contains(dsn, "127.0.0.1") || strings.Contains(dsn, "localhost"), "only explicit local test DB")
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	entropy := make([]byte, 8)
	_, err = rand.Read(entropy)
	require.NoError(t, err)
	schema := "qq_http_" + hex.EncodeToString(entropy)
	_, err = db.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	scoped, err := sql.Open("postgres", dsn+"&search_path="+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := scoped.Close(); err != nil {
			t.Error(err)
		}
		if _, err := db.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = scoped.Exec(`CREATE TABLE users(id BIGSERIAL PRIMARY KEY,email TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'active',deleted_at TIMESTAMPTZ,balance NUMERIC(20,8) NOT NULL DEFAULT 0,frozen_balance NUMERIC(20,8) NOT NULL DEFAULT 0,updated_at TIMESTAMPTZ NOT NULL DEFAULT now());INSERT INTO users(id,email,balance) VALUES(42,'owner@example.test',2),(99,'other@example.test',3)`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "248_qq_image_generation.sql"))
	require.NoError(t, err)
	_, err = scoped.Exec(string(migration))
	require.NoError(t, err)
	migration, err = os.ReadFile(filepath.Join("..", "..", "migrations", "249_qq_image_group_discovery.sql"))
	require.NoError(t, err)
	_, err = scoped.Exec(string(migration))
	require.NoError(t, err)
	migration, err = os.ReadFile(filepath.Join("..", "..", "migrations", "250_qq_reply_templates.sql"))
	require.NoError(t, err)
	_, err = scoped.Exec(string(migration))
	require.NoError(t, err)
	store := qqimage.NewStore(scoped, bytes.Repeat([]byte{3}, 32))
	conf := qqimage.DefaultConfig()
	conf.Enabled = true
	conf.AppID = "app"
	conf.BotSecret = "official-fixture-secret"
	conf.APIURL = "https://images.example.test/v1"
	conf.APIKey = "fixture-not-real"
	conf.Model = "fixture"
	conf.Size = "1024x1024"
	conf.Price = "1"
	conf.UserDailyAmount = "20"
	conf.AllowedGroups = []string{"group"}
	conf.PublicBaseURL = "https://sub.example.test"
	_, err = store.SaveConfig(context.Background(), conf, 1)
	require.NoError(t, err)
	sender := &qqHTTPIntegrationSender{}
	commands := qqimage.NewQQCommandHandler(store, sender)
	h := &QQImageHandler{store: store, email: &qqHTTPEmailStub{}}
	h.webhook = func(ctx context.Context, headers http.Header, body []byte) (any, error) {
		challenge, e := commands.HandleWebhook(ctx, headers, body)
		if challenge != nil {
			return challenge, e
		}
		return map[string]int{"op": 12}, e
	}
	send := func(message, content, secret string) *httptest.ResponseRecorder {
		t.Helper()
		now := time.Now()
		body, e := json.Marshal(map[string]any{"op": 0, "id": "event-" + message, "t": "GROUP_AT_MESSAGE_CREATE", "d": map[string]any{"id": message, "group_openid": "group", "content": content, "timestamp": now.Format(time.RFC3339), "author": map[string]string{"member_openid": "member"}}})
		require.NoError(t, e)
		seed := secret
		for len(seed) < 32 {
			seed += secret
		}
		key := ed25519.NewKeyFromSeed([]byte(seed[:32]))
		stamp := strconv.FormatInt(now.Unix(), 10)
		sig := ed25519.Sign(key, append([]byte(stamp), body...))
		req := httptest.NewRequest("POST", "/events", bytes.NewReader(body))
		req.Header.Set("X-Signature-Timestamp", stamp)
		req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
		r := gin.New()
		r.POST("/events", h.Events)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, 403, send("forged", "/绑定", "wrong-secret").Code)
	require.Equal(t, 200, send("bind", "/绑定", conf.BotSecret).Code)
	var requestID string
	require.NoError(t, scoped.QueryRow(`SELECT id FROM qq_image_binding_requests`).Scan(&requestID))
	verified := qqHTTPRequest(h, "POST", "/item/email", `{"code":"123456"}`, 42, "user", h.VerifyEmail)
	require.Equal(t, 200, verified.Code, verified.Body.String())
	authorized := qqHTTPRequest(h, "POST", "/item/"+requestID, `{"consent":true}`, 42, "user", h.AuthorizeBinding)
	require.Equal(t, 200, authorized.Code, authorized.Body.String())
	var reply struct {
		Data qqimage.BindingAuthorization `json:"data"`
	}
	require.NoError(t, json.Unmarshal(authorized.Body.Bytes(), &reply))
	require.NotEmpty(t, reply.Data.ConfirmationCode)
	require.Equal(t, 200, send("confirm", "/确认绑定 "+reply.Data.ConfirmationCode, conf.BotSecret).Code)
	require.Equal(t, 200, send("generate", "/生图 a tree", conf.BotSecret).Code)
	require.Equal(t, 200, send("generate", "/生图 a tree", conf.BotSecret).Code, "QQ retry must not charge/create twice")
	var orderID string
	var count int
	require.NoError(t, scoped.QueryRow(`SELECT count(*) FROM qq_image_orders`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, scoped.QueryRow(`SELECT id FROM qq_image_orders`).Scan(&orderID))
	// A mode switch blocks new events but must preserve already authorized orders and retrieval.
	h.cfg = &config.Config{RunMode: config.RunModeSimple}
	generator := &qqHTTPIntegrationGenerator{}
	worker := qqimage.NewWorker(store, generator, sender)
	require.NoError(t, worker.RunOnce(context.Background()))
	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, 1, generator.calls)
	require.Equal(t, 1, sender.sent)
	var balance, frozen string
	require.NoError(t, scoped.QueryRow(`SELECT balance::text,frozen_balance::text FROM users WHERE id=42`).Scan(&balance, &frozen))
	require.Equal(t, "1.00000000", balance)
	require.Equal(t, "0.00000000", frozen)
	private := qqHTTPRequest(h, "GET", "/item/"+orderID, "", 42, "user", h.Image)
	require.Equal(t, 200, private.Code)
	require.Equal(t, "image/png", private.Header().Get("Content-Type"))
	denied := qqHTTPRequest(h, "GET", "/item/"+orderID, "", 99, "user", h.Image)
	require.Equal(t, 404, denied.Code)
	var grantID string
	require.NoError(t, scoped.QueryRow(`SELECT id FROM qq_image_grants`).Scan(&grantID))
	revoked := qqHTTPRequest(h, "DELETE", "/item/"+grantID, "", 42, "user", h.RevokeGrant)
	require.Equal(t, 200, revoked.Code)
	identity := qqimage.Identity{Platform: "qq", AppID: "app", SubjectType: "group_member", SubjectID: "member", ScopeType: "group", ScopeID: "group", GroupID: "group"}
	_, err = store.GetOrderForIdentity(context.Background(), identity, orderID)
	require.Error(t, err)
	require.Equal(t, 200, qqHTTPRequest(h, "GET", "/item/"+orderID, "", 42, "user", h.Image).Code, "original website owner keeps the image")
}
