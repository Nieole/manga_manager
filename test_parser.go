package main

import (
	"fmt"
	"log"
	"manga-manager/internal/images"
	"manga-manager/internal/parser"
)

func main() {
	jobPath := "/Users/nicoer/comic/Angel Beats! -The Last Operation-/第４卷/第18話.zip"
	arc, err := parser.OpenArchive(jobPath)
	if err != nil {
		log.Fatalf("Open err: %v", err)
	}
	defer arc.Close()

	pages, err := arc.GetPages()
	if err != nil {
		log.Fatalf("GetPages err: %v", err)
	}
	fmt.Printf("Pages: %d\n", len(pages))
	if len(pages) > 0 {
		fmt.Printf("First page: %s, type: %s\n", pages[0].Name, pages[0].MediaType)
		pageData, err := arc.ReadPage(pages[0].Name)
		if err != nil {
			log.Fatalf("ReadPage err: %v", err)
		}
		fmt.Printf("Read bytes: %d\n", len(pageData))

		webpData, _, webpErr := images.ProcessImage(pageData, pages[0].MediaType, images.ProcessOptions{
			Width: 400, Quality: 82, Format: "webp",
		})
		if webpErr == nil {
			fmt.Printf("WebP success, bytes: %d\n", len(webpData))
		} else {
			fmt.Printf("WebP failure: %v\n", webpErr)
			jpegData, _, jpegErr := images.ProcessImage(pageData, pages[0].MediaType, images.ProcessOptions{
				Width: 400, Quality: 82, Format: "jpeg",
			})
			if jpegErr == nil {
				fmt.Printf("JPEG success, bytes: %d\n", len(jpegData))
			} else {
				fmt.Printf("JPEG failure: %v\n", jpegErr)
			}
		}
	}
}
