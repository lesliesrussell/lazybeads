// lb-17y
package domain

import "testing"

func FuzzDetectCycles(f *testing.F) {
	f.Add(byte(3), []byte{0, 1, 1, 2, 2, 0})
	f.Fuzz(func(t *testing.T, n byte, raw []byte) {
		nodes := int(n%16) + 1
		var edges []GraphEdge
		for i := 0; i+1 < len(raw) && len(edges) < 64; i += 2 {
			from := int(raw[i]) % nodes
			to := int(raw[i+1]) % nodes
			edges = append(edges, GraphEdge{
				FromID: string(rune('a' + from)),
				ToID:   string(rune('a' + to)),
				Type:   RelBlocks,
				Blocks: true,
			})
		}
		_ = DetectCycles(edges)
	})
}
