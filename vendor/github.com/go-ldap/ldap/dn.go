// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// File contains DN parsing functionallity
//
// https://tools.ietf.org/html/rfc4514
//
//   distinguishedName = [ relativeDistinguishedName
//         *( COMMA relativeDistinguishedName ) ]
//     relativeDistinguishedName = attributeTypeAndValue
//         *( PLUS attributeTypeAndValue )
//     attributeTypeAndValue = attributeType EQUALS attributeValue
//     attributeType = descr / numericoid
//     attributeValue = string / hexstring
//
//     ; The following characters are to be escaped when they appear
//     ; in the value to be encoded: ESC, one of <escaped>, leading
//     ; SHARP or SPACE, trailing SPACE, and NULL.
//     string =   [ ( leadchar / pair ) [ *( stringchar / pair )
//        ( trailchar / pair ) ] ]
//
//     leadchar = LUTF1 / UTFMB
//     LUTF1 = %x01-1F / %x21 / %x24-2A / %x2D-3A /
//        %x3D / %x3F-5B / %x5D-7F
//
//     trailchar  = TUTF1 / UTFMB
//     TUTF1 = %x01-1F / %x21 / %x23-2A / %x2D-3A /
//        %x3D / %x3F-5B / %x5D-7F
//
//     stringchar = SUTF1 / UTFMB
//     SUTF1 = %x01-21 / %x23-2A / %x2D-3A /
//        %x3D / %x3F-5B / %x5D-7F
//
//     pair = ESC ( ESC / special / hexpair )
//     special = escaped / SPACE / SHARP / EQUALS
//     escaped = DQUOTE / PLUS / COMMA / SEMI / LANGLE / RANGLE
//     hexstring = SHARP 1*hexpair
//     hexpair = HEX HEX
//
//  where the productions <descr>, <numericoid>, <COMMA>, <DQUOTE>,
//  <EQUALS>, <ESC>, <HEX>, <LANGLE>, <NULL>, <PLUS>, <RANGLE>, <SEMI>,
//  <SPACE>, <SHARP>, and <UTFMB> are defined in [RFC4512].
//

package ldap

import (
	"bytes"
	enchex "encoding/hex"
	"errors"
	"fmt"
	"strings"

	ber "gopkg.in/asn1-ber.v1"
)

// AttributeTypeAndValue represents an attributeTypeAndValue from https://tools.ietf.org/html/rfc4514
type AttributeTypeAndValue struct {
	// Type is the attribute type
	Type string
	// Value is the attribute value
	Value string
}

// RelativeDN represents a relativeDistinguishedName from https://tools.ietf.org/html/rfc4514
type RelativeDN struct {
	Attributes []*AttributeTypeAndValue
}

// DN represents a distinguishedName from https://tools.ietf.org/html/rfc4514
type DN struct {
	RDNs []*RelativeDN
}

// ParseDN returns a distinguishedName or an error
func ParseDN(str string) (*DN, error) {
	dn := new(DN)
	dn.RDNs = make([]*RelativeDN, 0)
	rdn := new(RelativeDN)
	rdn.Attributes = make([]*AttributeTypeAndValue, 0)
	buffer := bytes.Buffer{}
	attribute := new(AttributeTypeAndValue)
	escaping := false

	unescapedTrailingSpaces := 0
	stringFromBuffer := func() string {
		s := buffer.String()
		s = s[0 : len(s)-unescapedTrailingSpaces]
		buffer.Reset()
		unescapedTrailingSpaces = 0
		return s
	}

	for i := 0; i < len(str); i++ {
		char := str[i]
		if escaping {
			unescapedTrailingSpaces = 0
			escaping = false
			switch char {
			case ' ', '"', '#', '+', ',', ';', '<', '=', '>', '\\':
				buffer.WriteByte(char)
				continue
			}
			// Not a special character, assume hex encoded octet
			if len(str) == i+1 {
				return nil, errors.New("Got corrupted escaped character")
			}

			dst := []byte{0}
			n, err := enchex.Decode([]byte(dst), []byte(str[i:i+2]))
			if err != nil {
				return nil, fmt.Errorf("Failed to decode escaped character: %s", err)
			} else if n != 1 {
				return nil, fmt.Errorf("Expected 1 byte when un-escaping, got %d", n)
			}
			buffer.WriteByte(dst[0])
			i++
		} else if char == '\\' {
			unescapedTrailingSpaces = 0
			escaping = true
		} else if char == '=' {
			attribute.Type = stringFromBuffer()
			// Special case: If the first character in the value is # the
			// following data is BER encoded so we can just fast forward
			// and decode.
			if len(str) > i+1 && str[i+1] == '#' {
				i += 2
				index := strings.IndexAny(str[i:], ",+")
				data := str
				if index > 0 {
					data = str[i : i+index]
				} else {
					data = str[i:]
				}
				rawBER, err := enchex.DecodeString(data)
				if err != nil {
					return nil, fmt.Errorf("Failed to decode BER encoding: %s", err)
				}
				packet := ber.DecodePacket(rawBER)
				buffer.WriteString(packet.Data.String())
				i += len(data) - 1
			}
		} else if char == ',' || char == '+' {
			// We're done with this RDN or value, push it
			attribute.Value = stringFromBuffer()
			rdn.Attributes = append(rdn.Attributes, attribute)
			attribute = new(AttributeTypeAndValue)
			if char == ',' {
				dn.RDNs = append(dn.RDNs, rdn)
				rdn = new(RelativeDN)
				rdn.Attributes = make([]*AttributeTypeAndValue, 0)
			}
		} else if char == ' ' && buffer.Len() == 0 {
			// ignore unescaped leading spaces
			continue
		} else {
			if char == ' ' {
				// Track unescaped spaces in case they are trailing and we need to remove them
				unescapedTrailingSpaces++
			} else {
				// Reset if we see a non-space char
				unescapedTrailingSpaces = 0
			}
			buffer.WriteByte(char)
		}
	}
	if buffer.Len() > 0 {
		if len(attribute.Type) == 0 {
			return nil, errors.New("DN ended with incomplete type, value pair")
		}
		attribute.Value = stringFromBuffer()
		rdn.Attributes = append(rdn.Attributes, attribute)
		dn.RDNs = append(dn.RDNs, rdn)
	}
	return dn, nil
}
