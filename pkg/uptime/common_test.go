package uptime

import (
	"io/ioutil"
	"os"

	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

const testFile = "test.out"

func TestUptimeFromFileSuccess(t *testing.T) {
	d1 := []byte("350735.47 234388.90")
	ioutil.WriteFile(testFile, d1, 0644)

	value, err := UptimeFromFile(testFile)
	os.Remove(testFile)
	h.Ok(t, err)
	h.Equals(t, 350735, value)
}

func TestUptimeFromFileFailure(t *testing.T) {
	d1 := []byte("Something not time")
	ioutil.WriteFile(testFile, d1, 0644)

	_, err := UptimeFromFile(testFile)
	os.Remove(testFile)
	h.Assert(t, err != nil, "Failed to throw error for int64 parse")
}

