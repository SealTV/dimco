package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

var (
	configPath = flag.String("f", "config.json", "config file path")
)

func main() {
	c, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatal(err)
	}
	defer cli.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer cancel()

		cChan := make(chan os.Signal, 1)
		signal.Notify(cChan, syscall.SIGTERM, syscall.SIGINT)
		<-cChan
	}()

	wg := sync.WaitGroup{}
	defer wg.Wait()

	for _, image := range c.Images {
		wg.Add(1)
		go func(img ImageData) {
			defer wg.Done()

			fromImg := fmt.Sprintf("%v/%v%v:%v", c.FromRepo.BaseAddress, img.FromPrefix, img.Name, img.Tag)
			toImg := fmt.Sprintf("%v/%v%v:%v", c.ToRepo.BaseAddress, img.ToPrefix, img.Name, img.Tag)

			if err := pullImage(ctx, cli, fromImg, c.FromRepo); err != nil {
				log.Print(fmt.Errorf("can't pull image '%v': %w", fromImg, err))
				return
			}

			if err := tagImage(ctx, cli, fromImg, toImg); err != nil {
				log.Print(fmt.Errorf("can't tag image '%v', '%v': %w", fromImg, toImg, err))
				return
			}

			if err := pushImage(ctx, cli, toImg, c.ToRepo); err != nil {
				log.Print(fmt.Errorf("can't push image '%v': %w", toImg, err))
				return
			}

			if err := removeImages(ctx, cli, fromImg); err != nil {
				log.Print(fmt.Errorf("can't delete image '%v': %w", fromImg, err))
			}

			if err := removeImages(ctx, cli, toImg); err != nil {
				log.Print(fmt.Errorf("can't delete image '%v': %w", toImg, err))
			}

		}(image)
	}
}

func pullImage(ctx context.Context, cli *client.Client, image string, ac AuthConfig) error {
	out, err := cli.ImagePull(ctx, image, types.ImagePullOptions{
		All:          false,
		RegistryAuth: ac.ToEncodedString(),
	})
	if err != nil {
		return fmt.Errorf("can't pull image: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(os.Stdout, out); err != nil {
		return fmt.Errorf("can't copy image: %w", err)
	}

	return nil
}

func tagImage(ctx context.Context, cli *client.Client, fromImg, toImg string) error {
	if err := cli.ImageTag(ctx, fromImg, toImg); err != nil {
		return fmt.Errorf("can't tag image: %w", err)
	}

	return nil
}

func pushImage(ctx context.Context, cli *client.Client, image string, ac AuthConfig) error {
	reader, err := cli.ImagePush(ctx, image, types.ImagePushOptions{
		All:          false,
		RegistryAuth: ac.ToEncodedString(),
	})
	if err != nil {
		return fmt.Errorf("can't push image: %w", err)
	}
	defer reader.Close()

	if _, err := io.Copy(os.Stdout, reader); err != nil {
		return fmt.Errorf("can't copy image: %w", err)
	}

	return nil
}

func loadConfig(filepath string) (Config, error) {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		return Config{}, fmt.Errorf("can't read config file")
	}

	c := Config{}
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("can't unmarshal config")
	}

	return c, nil
}

func removeImages(ctx context.Context, cli *client.Client, img string) error {
	deletedItems, err := cli.ImageRemove(ctx, img, types.ImageRemoveOptions{
		Force:         true,
		PruneChildren: true,
	})
	if err != nil {
		return fmt.Errorf("can't tag image: %w", err)
	}

	log.Printf("delete images: %v", deletedItems)

	return nil
}

type Config struct {
	FromRepo AuthConfig  `json:"from_repo,omitempty"`
	ToRepo   AuthConfig  `json:"to_repo,omitempty"`
	Images   []ImageData `json:"images,omitempty"`
}

type AuthConfig struct {
	BaseAddress   string `json:"base_address,omitempty"`
	ServerAddress string `json:"server_address,omitempty"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
}

func (ac AuthConfig) ToEncodedString() string {
	authConfigBytes, _ := json.Marshal(ac)
	authConfigEncoded := base64.URLEncoding.EncodeToString(authConfigBytes)
	return authConfigEncoded
}

type ImageData struct {
	Name       string `json:"name,omitempty"`
	Tag        string `json:"tag,omitempty"`
	FromPrefix string `json:"from_prefix,omitempty"`
	ToPrefix   string `json:"to_prefix,omitempty"`
}
