// Package server assembles the inbound, egress and authenticated console listeners.
// Egress requests select an immutable proxy policy from their config snapshot.
// Custom/direct transports share a bounded reusable cache; policy changes and
// cache eviction preserve active requests, including streaming responses.
package server
