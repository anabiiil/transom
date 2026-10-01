package scan

import (
	"context"
	"path/filepath"
	"testing"
)

func TestApplicationRootsNeverProduceBundledDependencies(t *testing.T) {
	home := fakeHome(t)
	mac := filepath.Join(home, "Downloads", "Visual Studio Code.app")
	macProject := filepath.Join(mac, "Contents", "Resources", "app")
	portable := filepath.Join(home, "Tools", "VSCode")
	portableProject := filepath.Join(portable, "resources", "app")
	write(t, filepath.Join(portable, "Code.exe"), []byte("editor"))
	for _, p := range []string{macProject, portableProject} {
		write(t, filepath.Join(p, "package.json"), []byte(`{"name":"code"}`))
		write(t, filepath.Join(p, "node_modules", "dependency", "index.js"), []byte("application dependency"))
	}
	for _, root := range []string{mac, macProject, portable, portableProject, filepath.Dir(portable)} {
		e := &Env{Home: home, Opts: Options{Roots: []string{root}, LargeMinMB: 1}, Prog: NewProgress(), sem: make(chan struct{}, parallelism)}
		idx := e.homeIndex(context.Background(), []string{root})
		if len(idx.deps) != 0 || len(idx.large) != 0 {
			t.Errorf("application root %s produced cleanup items: %+v", root, idx)
		}
	}
}
