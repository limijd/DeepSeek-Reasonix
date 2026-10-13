package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/remotecloud"
	"reasonix/internal/session/control"
)

func TestCloudDesktopCannotImportHostPaths(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	outside := filepath.Join(testenv.TempDir(t), "secret.env")
	if err := os.WriteFile(outside, []byte("TEST_SECRET=local-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(HubOptions{})
	runtime := hubRuntime(t, hub, testenv.TempDir(t))
	runtime.Server.AllowLocalDesktop()
	base := "/rt/" + runtime.ID
	for _, path := range []string{outside, filepath.Join(filepath.Dir(outside), "missing"), filepath.Dir(outside)} {
		body, _ := json.Marshal(map[string]string{"path": path, "name": "copied.txt", "mime": "text/plain", "data": b64([]byte("uploaded"))})
		res, err := hub.CloudDesktop(t.Context(), remotecloud.DesktopRequest{Method: http.MethodPost, Path: base + "/attachments", Body: body}, "phone")
		if err != nil || res.Status != http.StatusNotFound || reasonOf(t, res.Body).Code != codeDeviceHostOnly {
			t.Fatalf("attachment %q: status=%d error=%v body=%s", path, res.Status, err, res.Body)
		}
	}
	body, _ := json.Marshal(map[string][]string{"paths": {filepath.Dir(outside)}})
	res, err := hub.CloudDesktop(t.Context(), remotecloud.DesktopRequest{Method: http.MethodPost, Path: base + "/drop", Body: body}, "phone")
	if err != nil || res.Status != http.StatusNotFound || reasonOf(t, res.Body).Code != codeDeviceHostOnly {
		t.Fatalf("drop: status=%d error=%v body=%s", res.Status, err, res.Body)
	}
	if entries, _ := os.ReadDir(filepath.Join(runtime.Root, ".reasonix", "attachments")); len(entries) != 0 {
		t.Fatalf("refused import stored %d attachments", len(entries))
	}
}

func TestCloudDesktopStillUploadsAttachmentBytes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	hub := NewHub(HubOptions{})
	runtime := hubRuntime(t, hub, testenv.TempDir(t))
	body, _ := json.Marshal(map[string]string{"name": "notes.txt", "mime": "text/plain", "data": b64([]byte("remote upload"))})
	res, err := hub.CloudDesktop(t.Context(), remotecloud.DesktopRequest{Method: http.MethodPost, Path: "/rt/" + runtime.ID + "/attachments", Body: body}, "phone")
	if err != nil || res.Status != http.StatusOK {
		t.Fatalf("upload: status=%d error=%v body=%s", res.Status, err, res.Body)
	}
	var stored struct{ Path string }
	if err := json.Unmarshal(res.Body, &stored); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(runtime.Root, filepath.FromSlash(stored.Path)))
	if err != nil || string(raw) != "remote upload" {
		t.Fatalf("stored=%q error=%v", raw, err)
	}
}

func TestPairedDeviceCannotImportHostPaths(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: testenv.TempDir(t)})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	server.AllowLocalDesktop()
	handler := operatorHandler(server)
	for path, body := range map[string]string{
		"/attachments": `{"path":"/host/private.env","name":"copy.txt","mime":"text/plain"}`,
		"/drop":        `{"paths":["/host/private"]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(withDeviceReach(req.Context(), "paired-phone", 1))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || reasonOf(t, rec.Body.Bytes()).Code != codeDeviceHostOnly {
			t.Fatalf("%s: status=%d body=%s", path, rec.Code, rec.Body)
		}
	}
}

func TestHostStillImportsAttachmentPaths(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "host.txt")
	if err := os.WriteFile(path, []byte("local attachment"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, reason := postAttachment(t, attachmentServer(t, testenv.TempDir(t)), map[string]string{"path": path, "name": "host.txt", "mime": "text/plain"})
	if status != http.StatusOK {
		t.Fatalf("host path: status=%d reason=%+v", status, reason)
	}
}

func TestNetworkServeCannotImportHostPaths(t *testing.T) {
	root := testenv.TempDir(t)
	outside := filepath.Join(testenv.TempDir(t), "private.env")
	if err := os.WriteFile(outside, []byte("TEST_SECRET=host-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: root})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{AuthMode: "token", Token: "test-client-token"})
	srv := httptest.NewServer(operatorHandler(server))
	defer srv.Close()
	status, reason := postAttachment(t, srv, map[string]string{"path": outside, "name": "copy.txt", "mime": "text/plain"})
	if status != http.StatusNotFound || reason.Code != codeDeviceHostOnly {
		t.Fatalf("serve token imported host path: status=%d reason=%+v", status, reason)
	}
	resp := postJSON(t, srv.URL+"/drop", map[string][]string{"paths": {filepath.Dir(outside)}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("serve token granted host folder access: status=%d", resp.StatusCode)
	}
	status, reason = postAttachment(t, srv, map[string]string{"name": "notes.txt", "mime": "text/plain", "data": b64([]byte("client upload"))})
	if status != http.StatusOK {
		t.Fatalf("serve token lost byte uploads: status=%d reason=%+v", status, reason)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, entry := range entries {
		if entry.Name() == ".gitignore" {
			continue
		}
		files++
		body, err := os.ReadFile(filepath.Join(root, ".reasonix", "attachments", entry.Name()))
		if err != nil || string(body) != "client upload" {
			t.Fatalf("unexpected imported bytes=%q error=%v", body, err)
		}
	}
	if files != 1 {
		t.Fatalf("stored %d files, want only the byte upload", files)
	}
}
