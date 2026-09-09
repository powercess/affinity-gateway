package sessionaffinity

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"strconv"
)

func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		seen := map[string]bool{}
		for d.NextBlock(0) {
			key := d.Val()
			if seen[key] {
				return d.Errf("duplicate option %s", key)
			}
			seen[key] = true
			args := d.RemainingArgs()
			if len(args) != 1 {
				return d.ArgErr()
			}
			v := args[0]
			switch key {
			case "observe_id":
				h.ObserveID = v
			case "mode":
				h.Mode = v
			case "secret_env":
				h.SecretEnv = v
			case "identity_scope":
				h.IdentityScope = v
			case "identity_source":
				h.IdentitySource = v
			case "output_header":
				h.OutputHeader = v
			case "policy":
				h.Policy = v
			case "missing":
				h.Missing = v
			case "fallback":
				h.Fallback = v
			case "body_limit":
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return d.Err("body_limit must be bytes")
				}
				h.BodyLimit = n
			case "cache_key_as_session":
				b, err := strconv.ParseBool(v)
				if err != nil {
					return d.Err("cache_key_as_session must be boolean")
				}
				h.CacheKeyAsSession = b
			default:
				return d.Errf("unknown option %s", key)
			}
		}
	}
	return nil
}
