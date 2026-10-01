package ui

// DiskInfo describes the volume containing the current user's home.
type DiskInfo struct {
	Total  int64  `json:"total"`
	Free   int64  `json:"free"`
	Used   int64  `json:"used"`
	Volume string `json:"volume"`
}
