package tools

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"strings"
)

type hashGeneratorArgs struct {
	Input     string `json:"input"`
	Algorithm string `json:"algorithm"`
}

func NewHashGenerator() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "The string to hash.",
			},
			"algorithm": map[string]any{
				"type":        "string",
				"enum":        []string{"md5", "sha1", "sha256", "sha512"},
				"description": "The hash algorithm to use: md5, sha1, sha256, or sha512.",
			},
		},
		"required": []string{"input", "algorithm"},
	}

	return NewFuncTool(
		"hash_generator",
		"Generate cryptographic hashes (MD5, SHA1, SHA256, SHA512) of input strings for checksums and verification.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in hashGeneratorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid hash_generator args: %w", err)
			}
			if in.Input == "" {
				return nil, fmt.Errorf("input is required")
			}

			var h hash.Hash
			algorithm := strings.ToLower(in.Algorithm)

			switch algorithm {
			case "md5":
				h = md5.New()
			case "sha1":
				h = sha1.New()
			case "sha256":
				h = sha256.New()
			case "sha512":
				h = sha512.New()
			default:
				return nil, fmt.Errorf("unsupported algorithm %q, use: md5, sha1, sha256, or sha512", in.Algorithm)
			}

			h.Write([]byte(in.Input))
			hashBytes := h.Sum(nil)
			hashHex := hex.EncodeToString(hashBytes)

			return map[string]any{
				"hash":      hashHex,
				"algorithm": algorithm,
				"length":    len(hashHex),
			}, nil
		},
	)
}

// HashMultiple generates hashes using multiple algorithms at once.
func HashMultiple(input string) map[string]string {
	return map[string]string{
		"md5":    hashWith(md5.New(), input),
		"sha1":   hashWith(sha1.New(), input),
		"sha256": hashWith(sha256.New(), input),
		"sha512": hashWith(sha512.New(), input),
	}
}

func hashWith(h hash.Hash, input string) string {
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyHash checks if an input matches a given hash.
func VerifyHash(input, expectedHash, algorithm string) bool {
	var h hash.Hash
	switch strings.ToLower(algorithm) {
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return false
	}

	h.Write([]byte(input))
	actualHash := hex.EncodeToString(h.Sum(nil))
	return strings.EqualFold(actualHash, expectedHash)
}
