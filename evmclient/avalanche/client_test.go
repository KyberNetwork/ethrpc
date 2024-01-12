package avalanche

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClient_BlockNumber(t *testing.T) {
	t.Run("it should return correct block number", func(t *testing.T) {
		client, err := NewClient("https://1rpc.io/avax/c")

		assert.Nil(t, err)

		blockNumber, err := client.BlockNumber(context.Background())

		fmt.Printf("blockNumber: %d\n", blockNumber)

		assert.Nil(t, err)
		assert.Greater(t, blockNumber, 0)
	})
}
