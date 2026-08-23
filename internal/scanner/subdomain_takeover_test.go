package scanner

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildTakeoverVulnerability(t *testing.T) {
	t.Run("fingerprint confirmed", func(t *testing.T) {
		vul := buildTakeoverVulnerability(&TakeoverResult{
			Subdomain:   "old.example.com",
			CName:       "org.github.io",
			Vulnerable:  true,
			Service:     "github",
			Fingerprint: "There isn't a GitHub Pages site here",
		})
		assert.Equal(t, "subdomain_takeover", vul.Source)
		assert.Equal(t, "old.example.com", vul.Host)
		assert.Equal(t, 80, vul.Port)
		assert.Equal(t, "http://old.example.com", vul.Url)
		assert.Equal(t, "subdomain-takeover", vul.PocFile)
		assert.Equal(t, "high", vul.Severity)
		assert.Equal(t, "Subdomain Takeover", vul.VulName)
		assert.Contains(t, vul.Result, "org.github.io")
		assert.Contains(t, vul.Extra, "fingerprint=There isn't a GitHub Pages site here")
		assert.Contains(t, vul.Tags, "subdomain-takeover")
	})

	t.Run("dangling cname downgraded to medium", func(t *testing.T) {
		vul := buildTakeoverVulnerability(&TakeoverResult{
			Subdomain:   "dangling.example.com",
			CName:       "gone.herokuapp.com",
			Vulnerable:  true,
			Service:     "unknown",
			Fingerprint: "CNAME exists but target is unresolvable",
		})
		assert.Equal(t, "medium", vul.Severity)
		assert.Contains(t, vul.Result, "Dangling CNAME")
	})

	t.Run("dedup key stable across evidence schemes", func(t *testing.T) {
		// 重扫去重键为 host+port+pocFile+url，两次命中指纹的服务不同也必须生成相同键
		a := buildTakeoverVulnerability(&TakeoverResult{Subdomain: "a.example.com", CName: "x.github.io", Vulnerable: true, Service: "github", Fingerprint: "f1"})
		b := buildTakeoverVulnerability(&TakeoverResult{Subdomain: "a.example.com", CName: "y.herokuapp.com", Vulnerable: true, Service: "heroku", Fingerprint: "f2"})
		key := func(v *Vulnerability) string { return fmt.Sprintf("%s:%d:%s:%s", v.Host, v.Port, v.PocFile, v.Url) }
		assert.Equal(t, key(a), key(b))
	})
}
