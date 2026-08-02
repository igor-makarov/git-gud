package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

type remoteRef struct {
	name   plumbing.ReferenceName
	hash   plumbing.Hash
	target plumbing.ReferenceName
	peeled plumbing.Hash
	unborn bool
}

type lsRefsResponse struct {
	refs []remoteRef
}

func (response *lsRefsResponse) Decode(reader io.Reader) error {
	for {
		length, packet, err := pktline.ReadLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if length == pktline.Flush || length == pktline.ResponseEnd {
			return nil
		}
		fields := strings.Fields(strings.TrimSuffix(string(packet), "\n"))
		if len(fields) < 2 {
			return fmt.Errorf("malformed ls-refs response")
		}
		ref := remoteRef{name: plumbing.ReferenceName(fields[1])}
		if fields[0] == "unborn" {
			ref.unborn = true
		} else {
			if !plumbing.IsHash(fields[0]) {
				return fmt.Errorf("remote returned invalid object ID %q", fields[0])
			}
			ref.hash, _ = plumbing.FromHex(fields[0])
		}
		for _, attribute := range fields[2:] {
			switch {
			case strings.HasPrefix(attribute, "symref-target:"):
				ref.target = plumbing.ReferenceName(strings.TrimPrefix(attribute, "symref-target:"))
			case strings.HasPrefix(attribute, "peeled:"):
				value := strings.TrimPrefix(attribute, "peeled:")
				if !plumbing.IsHash(value) {
					return fmt.Errorf("remote returned invalid peeled object ID %q", value)
				}
				ref.peeled, _ = plumbing.FromHex(value)
			}
		}
		response.refs = append(response.refs, ref)
	}
}

func listRemoteRefs(ctx context.Context, commander transport.Commander, prefixes []string) ([]remoteRef, error) {
	arguments := &packp.LsRefsArgs{
		Peel:        true,
		Symrefs:     true,
		Unborn:      true,
		RefPrefixes: prefixes,
	}
	response := &lsRefsResponse{}
	if err := commander.Command(ctx, "ls-refs", arguments, response); err != nil {
		return nil, err
	}
	return response.refs, nil
}

func resolveRef(ctx context.Context, commander transport.Commander, requested string) (plumbing.Hash, plumbing.ReferenceName, error) {
	if requested != "" && plumbing.IsHash(requested) {
		hash, _ := plumbing.FromHex(requested)
		return hash, "", nil
	}

	if requested == "" || requested == "HEAD" {
		refs, err := listRemoteRefs(ctx, commander, []string{"HEAD"})
		if err != nil {
			return plumbing.ZeroHash, "", fmt.Errorf("resolve remote HEAD: %w", err)
		}
		for _, ref := range refs {
			if ref.name != plumbing.HEAD {
				continue
			}
			if ref.unborn || ref.hash.IsZero() {
				return plumbing.ZeroHash, "", fmt.Errorf("remote HEAD is unborn")
			}
			if ref.target != "" {
				return ref.hash, ref.target, nil
			}
			return ref.hash, plumbing.HEAD, nil
		}
		return plumbing.ZeroHash, "", fmt.Errorf("remote did not advertise HEAD")
	}

	var candidates []plumbing.ReferenceName
	if strings.HasPrefix(requested, "refs/") {
		candidates = []plumbing.ReferenceName{plumbing.ReferenceName(requested)}
	} else {
		candidates = []plumbing.ReferenceName{
			plumbing.ReferenceName("refs/heads/" + requested),
			plumbing.ReferenceName("refs/tags/" + requested),
		}
	}
	prefixes := make([]string, len(candidates))
	for index, candidate := range candidates {
		prefixes[index] = candidate.String()
	}
	refs, err := listRemoteRefs(ctx, commander, prefixes)
	if err != nil {
		return plumbing.ZeroHash, "", fmt.Errorf("resolve ref %q: %w", requested, err)
	}
	for _, candidate := range candidates {
		for _, ref := range refs {
			if ref.name != candidate || ref.unborn {
				continue
			}
			if !ref.peeled.IsZero() {
				return ref.peeled, candidate, nil
			}
			return ref.hash, candidate, nil
		}
	}
	return plumbing.ZeroHash, "", fmt.Errorf("ref %q not found", requested)
}
