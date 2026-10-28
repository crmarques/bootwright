package secrets

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
)

func CertificateBundle(data []byte) ([]*x509.Certificate, string) {
	remainder := TrimPEMWhitespace(data)
	certificates := []*x509.Certificate{}
	for len(remainder) > 0 {
		block, rest, ok := StrictPEMBlock(remainder)
		if !ok {
			return nil, "certificate material must contain only complete PEM certificate blocks"
		}
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, "certificate material contains an invalid PEM block"
		}
		parsed, err := x509.ParseCertificates(block.Bytes)
		if err != nil || len(parsed) != 1 {
			return nil, "certificate material contains an invalid X.509 certificate"
		}
		certificates = append(certificates, parsed[0])
		remainder = TrimPEMWhitespace(rest)
	}
	if len(certificates) == 0 {
		return nil, "certificate material is empty"
	}
	return certificates, ""
}

func StrictPEMBlock(data []byte) (*pem.Block, []byte, bool) {
	data = bytes.TrimLeft(data, " \t\r\n\v\f")
	const beginPrefix = "-----BEGIN "
	const lineSuffix = "-----"
	if !bytes.HasPrefix(data, []byte(beginPrefix)) {
		return nil, data, false
	}
	beginEnd := bytes.IndexByte(data, '\n')
	if beginEnd < 0 {
		return nil, data, false
	}
	beginLine := bytes.TrimSuffix(data[:beginEnd], []byte{'\r'})
	if !bytes.HasSuffix(beginLine, []byte(lineSuffix)) {
		return nil, data, false
	}
	typeName := beginLine[len(beginPrefix) : len(beginLine)-len(lineSuffix)]
	if len(typeName) == 0 || bytes.ContainsAny(typeName, "\r\n") {
		return nil, data, false
	}
	endMarker := append([]byte("\n-----END "), typeName...)
	endMarker = append(endMarker, []byte(lineSuffix)...)
	endStart := bytes.Index(data, endMarker)
	if endStart < 0 || bytes.Contains(data[beginEnd:endStart], []byte("\n-----BEGIN ")) {
		return nil, data, false
	}
	afterMarker := endStart + len(endMarker)
	lineEnd := bytes.IndexByte(data[afterMarker:], '\n')
	consumed := len(data)
	if lineEnd >= 0 {
		consumed = afterMarker + lineEnd + 1
	}
	if len(bytes.Trim(data[afterMarker:consumed], " \t\r\n")) != 0 {
		return nil, data, false
	}
	blockBytes := data[:consumed]
	block, ignored := pem.Decode(blockBytes)
	if block == nil || len(TrimPEMWhitespace(ignored)) != 0 || block.Type != string(typeName) {
		return nil, data, false
	}
	return block, data[consumed:], true
}

func TrimPEMWhitespace(data []byte) []byte {
	return bytes.Trim(data, " \t\r\n\v\f")
}
