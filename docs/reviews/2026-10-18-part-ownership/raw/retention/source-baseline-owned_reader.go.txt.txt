package storage

import "io"

func readOwnedBytes(reader io.Reader) ([]byte, error) {
	var hint int64 = 512
	if sized, ok := reader.(interface{ ReadSize() int64 }); ok {
		hint = sized.ReadSize()
	} else if sized, ok := reader.(interface{ Len() int }); ok {
		hint = int64(sized.Len())
	}
	if hint < 0 {
		hint = 512
	}
	if hint > 32<<20 {
		hint = 32 << 20
	}
	data := make([]byte, 0, int(hint)+1)
	for {
		n, err := reader.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
		if len(data) == cap(data) {
			data = append(data, 0)[:len(data)]
		}
	}
}
