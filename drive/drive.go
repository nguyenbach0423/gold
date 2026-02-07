package drive

import (
	"context"
	"io"
	"os"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type Drive struct {
	service *drive.Service
}

func New(env string, credentials string) (*Drive, error) {
	var service *drive.Service
	var err error

	if env != "PROD" {
		if service, err = drive.NewService(
			context.Background(),
			option.WithAuthCredentialsFile(
				option.ServiceAccount,
				"cre.json",
			),
		); err != nil {
			return nil, err
		}

		return &Drive{service: service}, nil
	}

	if service, err = drive.NewService(
		context.Background(),
		option.WithAuthCredentialsJSON(
			option.ServiceAccount,
			[]byte(credentials),
		),
	); err != nil {
		return nil, err
	}

	return &Drive{service: service}, nil
}

func (d *Drive) UploadFile(path, fileID string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = d.service.Files.Update(fileID, nil).Media(f).Do()
	return err
}

func (d *Drive) DownloadFile(fileID, path string) error {
	resp, err := d.service.Files.Get(fileID).Download()
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = io.Copy(f, resp.Body)
	return err
}
