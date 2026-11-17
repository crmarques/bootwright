package acquisition

import "strings"

const digestSeparator = "@sha256:"

type ImageReference struct {
	Repository string
	Tag        string
	Digest     string
}

func ParseImageReference(reference string) ImageReference {
	repository, digest, _ := strings.Cut(reference, digestSeparator)
	parsed := ImageReference{Repository: repository, Digest: digest}
	if separator := strings.LastIndex(repository, ":"); separator > strings.LastIndex(repository, "/") {
		parsed.Repository, parsed.Tag = repository[:separator], repository[separator+1:]
	}
	return parsed
}

func (r ImageReference) Qualified() ImageReference {
	if first, _, _ := strings.Cut(r.Repository, "/"); !strings.ContainsAny(first, ".:") && first != "localhost" {
		r.Repository = "docker.io/" + r.Repository
	}
	return r
}

func (r ImageReference) String() string {
	reference := r.Repository
	if r.Tag != "" {
		reference += ":" + r.Tag
	}
	if r.Digest != "" {
		reference += digestSeparator + r.Digest
	}
	return reference
}

func (r ImageReference) Pinned() string {
	return r.Repository + digestSeparator + r.Digest
}
