package cli

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"vaulty-keeper/internal/dbproxy"
)

func TestMongoConnectCommand(t *testing.T) {
	out := captureStdout(t, func() {
		printConnCommand("mongodb", "tok' $", "::1", 27018, "db space")
	})
	if !strings.HasPrefix(out, "mongosh '") {
		t.Fatalf("missing mongosh command: %q", out)
	}
	_, links, err := dbproxy.TunnelLinks("mongodb", "tok' $", "::1", 27018, "db space", nil)
	if err != nil || len(links) != 1 || out != links[0].Value+"\n" {
		t.Fatalf("command must come from common builder: %q, %v", out, err)
	}
}

func TestMongoProbeOutput(t *testing.T) {
	i18nTest(t)
	conn := dbproxy.Conn{Name: "mongo", Type: "mongodb", URL: "mongodb://upstreamUser:upstreamPassword@private.example/orders"}
	out := captureStdout(t, func() { printDBTestOK(conn) })
	if !strings.Contains(out, "mongodb") || !strings.Contains(out, "orders") || !strings.Contains(out, "OK") {
		t.Fatalf("missing probe result: %q", out)
	}
	for _, secret := range []string{"upstreamUser", "upstreamPassword", "private.example"} {
		if strings.Contains(out, secret) {
			t.Fatalf("probe leaked %s", secret)
		}
	}
	conn.Type, conn.URL = "postgres", "postgres://app:pw@host/orders"
	if out := captureStdout(t, func() { printDBTestOK(conn) }); !strings.Contains(out, "app") {
		t.Fatalf("changed other protocol metadata: %q", out)
	}
}

func TestMongoShellCommand(t *testing.T) {
	const envKey = "VAULTY_KEEPER_MONGODB_URI"
	t.Setenv(envKey, "parent-value")
	const raw = "mongodb://syntheticUser:p%27ass@private.example:27017/orders?authSource=admin&tls=true"
	cmd, err := shellCommand(dbproxy.Conn{Type: "mongodb", URL: raw})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 5 || !reflect.DeepEqual(cmd.Args[:4], []string{"mongosh", "--nodb", "--shell", "--eval"}) {
		t.Fatalf("unexpected command arguments: %q", cmd.Args)
	}
	for _, secret := range []string{raw, "syntheticUser", "p%27ass", "private.example", "orders"} {
		if strings.Contains(strings.Join(cmd.Args, " "), secret) {
			t.Fatalf("argv leaked %s", secret)
		}
	}
	count := 0
	for _, item := range cmd.Env {
		if strings.HasPrefix(item, envKey+"=") {
			count++
			if item != envKey+"="+raw {
				t.Fatal("incorrect subprocess URI")
			}
		}
	}
	if count != 1 || os.Getenv(envKey) != "parent-value" {
		t.Fatal("URI must exist only once in child env; parent must stay unchanged")
	}
	if cmd.Stdin != os.Stdin || cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Fatal("interactive stdio not attached")
	}
	other, err := shellCommand(dbproxy.Conn{Type: "mongodb", URL: "mongodb://other:secret@elsewhere/test"})
	if err != nil || !reflect.DeepEqual(cmd.Args, other.Args) {
		t.Fatal("startup script must be constant across connections")
	}

	// Execute only the JavaScript startup contract, never a real mongosh or DB.
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable for startup script validation")
	}
	const harness = `const vm = require('node:vm');
const assert = require('node:assert/strict');
const expected = process.env.VAULTY_KEEPER_MONGODB_URI;
(async () => {
  for (const fail of [false, true]) {
    process.env.VAULTY_KEEPER_MONGODB_URI = expected;
    let called = false;
    const result = {};
    const context = { process, db: null, connect: async uri => {
      called = true;
      assert.equal(uri, expected);
      assert.equal(process.env.VAULTY_KEEPER_MONGODB_URI, undefined);
      if (fail) throw new Error('synthetic failure');
      return result;
    }};
    try {
      await vm.runInNewContext('(async () => { ' + process.argv[1] + ' })()', context);
      assert.equal(fail, false);
      assert.equal(context.db, result);
    } catch (err) {
      if (!fail || err.message !== 'synthetic failure') throw err;
    }
    assert.equal(called, true);
    assert.equal(process.env.VAULTY_KEEPER_MONGODB_URI, undefined);
    assert.equal(context.uri, undefined);
  }
})().catch(err => { console.error(err); process.exitCode = 1; });`
	check := exec.Command(node, "-e", harness, cmd.Args[4])
	check.Env = cmd.Env
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("startup script failed: %v: %s", err, out)
	}
}

func TestMongoDefaultPort(t *testing.T) {
	host, port := splitHostPort("localhost", "mongodb")
	if host != "localhost" || port != "27017" {
		t.Fatalf("address = %s:%s", host, port)
	}
}

func TestShellCommandPreservesOtherProtocols(t *testing.T) {
	for _, tc := range []struct {
		typ, raw string
		args     []string
		env      []string
	}{
		{"postgres", "postgres://app:synthetic@localhost/orders", []string{"psql"}, []string{"PGHOST=localhost", "PGPORT=5432", "PGUSER=app", "PGPASSWORD=synthetic", "PGDATABASE=orders"}},
		{"mysql", "mysql://app:synthetic@[::1]:3307/orders", []string{"mysql", "-h", "::1", "-P", "3307", "-u", "app", "orders"}, []string{"MYSQL_PWD=synthetic"}},
		{"redis", "redis://:synthetic@localhost/2", []string{"redis-cli", "-h", "localhost", "-p", "6379", "-n", "2"}, []string{"REDISCLI_AUTH=synthetic"}},
		{"redis", "redis://:synthetic@localhost/0", []string{"redis-cli", "-h", "localhost", "-p", "6379"}, []string{"REDISCLI_AUTH=synthetic"}},
	} {
		t.Run(tc.typ+tc.raw, func(t *testing.T) {
			cmd, err := shellCommand(dbproxy.Conn{Type: tc.typ, URL: tc.raw})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cmd.Args, tc.args) || !reflect.DeepEqual(cmd.Env[len(cmd.Env)-len(tc.env):], tc.env) {
				t.Fatal("existing client arguments or environment changed")
			}
		})
	}
}
