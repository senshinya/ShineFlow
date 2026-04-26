package credential_test

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domaincredential "github.com/shinya/shineflow/domain/credential"
	storagecredential "github.com/shinya/shineflow/infrastructure/storage/credential"
	"github.com/shinya/shineflow/infrastructure/storage/storagetest"
	"github.com/shinya/shineflow/infrastructure/util"
)

func newCred(t *testing.T) *domaincredential.Credential {
	t.Helper()
	return &domaincredential.Credential{
		ID:               uuid.NewString(),
		Name:             "openai-key-" + uuid.NewString(),
		Kind:             domaincredential.CredentialKindAPIKey,
		EncryptedPayload: []byte{1, 2, 3, 4},
		CreatedBy:        "u",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
}

func TestCredential_CreateAndGet(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecredential.NewCredentialRepository()
	c := newCred(t)
	if err := repo.Create(ctx, c); err != nil { t.Fatal(err) }

	got, err := repo.Get(ctx, c.ID)
	if err != nil { t.Fatal(err) }
	if got.Name != c.Name { t.Fatalf("name: %s", got.Name) }
}

func TestCredential_GetNotFound(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecredential.NewCredentialRepository()
	_, err := repo.Get(ctx, uuid.NewString())
	if !errors.Is(err, domaincredential.ErrCredentialNotFound) {
		t.Fatalf("expected ErrCredentialNotFound: %v", err)
	}
}

func TestCredential_DeleteSoft(t *testing.T) {
	ctx := storagetest.Setup(t)
	repo := storagecredential.NewCredentialRepository()
	c := newCred(t); _ = repo.Create(ctx, c)
	if err := repo.Delete(ctx, c.ID); err != nil { t.Fatal(err) }
	_, err := repo.Get(ctx, c.ID)
	if !errors.Is(err, domaincredential.ErrCredentialNotFound) {
		t.Fatalf("expected NotFound after soft delete: %v", err)
	}
}

func TestResolver_RoundTrip(t *testing.T) {
	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i)
	}
	keyB64 := base64.StdEncoding.EncodeToString(keyBytes)
	t.Setenv("SHINEFLOW_CRED_KEY", keyB64)

	repo := storagecredential.NewCredentialRepository()
	resolver, err := storagecredential.NewResolver(repo)
	if err != nil { t.Fatalf("new resolver: %v", err) }

	payload := domaincredential.Payload{"key": "sk-xxx"}
	payloadJSON, _ := util.MarshalToString(payload)
	cipher, err := storagecredential.Encrypt(keyBytes, []byte(payloadJSON))
	if err != nil { t.Fatal(err) }

	ctx := storagetest.Setup(t)
	c := newCred(t); c.EncryptedPayload = cipher
	_ = repo.Create(ctx, c)

	gotCred, gotPayload, err := resolver.Resolve(ctx, c.ID)
	if err != nil { t.Fatalf("resolve: %v", err) }
	if gotCred.ID != c.ID { t.Fatalf("cred id: %s", gotCred.ID) }
	if gotPayload["key"] != "sk-xxx" {
		t.Fatalf("payload: %+v", gotPayload)
	}
}

func TestResolver_MissingEnvKey(t *testing.T) {
	t.Setenv("SHINEFLOW_CRED_KEY", "")
	repo := storagecredential.NewCredentialRepository()
	if _, err := storagecredential.NewResolver(repo); err == nil {
		t.Fatal("expected error when env key missing")
	}
}

func TestResolver_BadKeyLength(t *testing.T) {
	t.Setenv("SHINEFLOW_CRED_KEY", base64.StdEncoding.EncodeToString([]byte("too-short")))
	repo := storagecredential.NewCredentialRepository()
	if _, err := storagecredential.NewResolver(repo); err == nil {
		t.Fatal("expected error for non-32B key")
	}
}
