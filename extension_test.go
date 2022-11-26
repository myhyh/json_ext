package json_ext

import (
	"log"
	"testing"
)

type extTestStruct struct {
	I int64
}

func TestExtensionEncode(t *testing.T) {
	Register(extTestStruct{})
	b, err := Marshal(extTestStruct{1})
	if err != nil {
		log.Fatal(err)
	}

	log.Println(string(b))
}

func TestExtensionDecode(t *testing.T) {
	j := `{"@type@":"github.com/myhyh/json_ext.extTestStruct","I":1}`
	Register(extTestStruct{})
	z := interface{}(nil)
	err := Unmarshal([]byte(j), &z)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("%+v", z)
}
