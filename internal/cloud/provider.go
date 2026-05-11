package cloud

type Provider int

const (
	AWS Provider = iota
	Azure
	GCP
)

type Info struct {
	Name        Provider
	Region      string
	AccountInfo map[string]string
}

type InfoLoader interface {
	GetInfo() (*Info, error)
}
