package k8s

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/airbytehq/abctl/internal/paths"
	"github.com/google/go-cmp/cmp"
)

func TestProvider_Defaults(t *testing.T) {
	t.Run("DefaultProvider", func(t *testing.T) {
		if d := cmp.Diff(Kind, DefaultProvider.Name); d != "" {
			t.Errorf("Name mismatch (-want +got):\n%s", d)
		}
		if d := cmp.Diff("airbyte-abctl", DefaultProvider.ClusterName); d != "" {
			t.Errorf("ClusterName mismatch (-want +got):\n%s", d)
		}
		if d := cmp.Diff("kind-airbyte-abctl", DefaultProvider.Context); d != "" {
			t.Errorf("Context mismatch (-want +got):\n%s", d)
		}
		if d := cmp.Diff(paths.Kubeconfig, DefaultProvider.Kubeconfig); d != "" {
			t.Errorf("Kubeconfig mismatch (-want +got):\n%s", d)
		}
	})

	t.Run("test", func(t *testing.T) {
		if d := cmp.Diff(Test, TestProvider.Name); d != "" {
			t.Errorf("Name mismatch (-want +got):\n%s", d)
		}
		if d := cmp.Diff("test-airbyte-abctl", TestProvider.ClusterName); d != "" {
			t.Errorf("ClusterName mismatch (-want +got):\n%s", d)
		}
		if d := cmp.Diff("test-airbyte-abctl", TestProvider.Context); d != "" {
			t.Errorf("Context mismatch (-want +got):\n%s", d)
		}
		// the TestProvider uses a temporary directory, so verify
		// - filename is correct
		// - directory is not paths.Kubeconfig
		if d := cmp.Diff(paths.FileKubeconfig, filepath.Base(TestProvider.Kubeconfig)); d != "" {
			t.Errorf("Kubeconfig mismatch (-want +got):\n%s", d)
		}
		if d := cmp.Diff(paths.Kubeconfig, TestProvider.Kubeconfig); d == "" {
			t.Errorf("Kubeconfig should differ (%s)", paths.Kubeconfig)
		}
	})
}

func TestProvider_Cluster(t *testing.T) {
	// go will reuse TempDir directories between runs, ensure it is clean before running this test
	if err := os.RemoveAll(filepath.Dir(TestProvider.Kubeconfig)); err != nil {
		t.Fatalf("failed to remove temp kubeconfig dir: %s", err)
	}

	if dirExists(filepath.Dir(TestProvider.Kubeconfig)) {
		t.Fatal("Kubeconfig should not exist")
	}

	cluster, err := TestProvider.Cluster(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !dirExists(filepath.Dir(TestProvider.Kubeconfig)) {
		t.Error("Kubeconfig should exist")
	}

	if cluster == nil {
		t.Error("cluster should not be nil")
	}
}

func dirExists(dir string) bool {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return false
	} else if err != nil {
		return false
	}

	return true
}
