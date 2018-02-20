package seal

import (
	"io"
)

type ProofOfReplication struct{}

type Sealer interface {
	Seal(r io.ReadSeeker, w io.WriteSeeker) (*ProofOfReplication, error)
	ReadAt(r io.ReadSeeker, offset uint64, b []byte) (int, error)
	Verify(r io.ReadSeeker, p *ProofOfReplication) error
}

type NullSealer struct{}

func (ns NullSealer) Seal(r io.ReadSeeker, w io.WriteSeeker) error {
	_, err := io.Copy(w, r)
	return new(ProofOfReplication), err
}

func (ns NullSealer) ReadAt(r io.ReadSeeker, offset uint64, b []byte) (int, error) {
	_, err := r.Seek(offset, io.SeekStart)
	if err != nil {
		return 0, err
	}

	return r.Read(b)
}

func (ns NullSealer) Verify(r io.ReadSeeker, p *ProofOfReplication) error {
	return nil
}
