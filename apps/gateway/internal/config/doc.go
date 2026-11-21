// Package config owns atomic JSON persistence and immutable runtime snapshots.
// Global egress_proxy defaults to environment for older configuration files.
// Egress routes may inherit that policy or override it with direct/custom;
// inbound forwarding keeps the standard environment proxy behavior.
// Proxy URL credentials are stored in the restricted configuration file and
// returned only through the authenticated control API, never in errors.
package config
