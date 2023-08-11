package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"jiso/internal/cli"
	cmd "jiso/internal/command"
	cfg "jiso/internal/config"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/padding"
	"github.com/moov-io/iso8583/prefix"
	"github.com/moov-io/iso8583/sort"
	"github.com/moov-io/iso8583/specs"
)

func main() {
	play()
	os.Exit(0)

	err := cfg.GetConfig().Parse()
	if err != nil {
		fmt.Printf("Error parsing config: %s\n", err)
		os.Exit(1)
	}

	cli := cli.NewCLI()

	// Handle kill and interrupt signals to close the service's connection gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cli.Close()
		fmt.Println("Exiting CLI tool")
		os.Exit(0)
	}()

	cli.ClearTerminal()

	if cfg.GetConfig().GetHost() == "" ||
		cfg.GetConfig().GetPort() == "" ||
		cfg.GetConfig().GetSpec() == "" ||
		cfg.GetConfig().GetFile() == "" {
		cli.AddCommand(&cmd.CollectArgsCommand{})
	}

	err = cli.Run()
	if err != nil {
		fmt.Printf("Error running CLI: %s\n", err)
	}
}

func play() {
	spec := &iso8583.MessageSpec{
		Fields: map[int]field.Field{
			0: field.NewString(&field.Spec{
				Length:      4,
				Description: "Message Type Indicator",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			1: field.NewBitmap(&field.Spec{
				Description: "Bitmap",
				Enc:         encoding.BytesToASCIIHex,
				Pref:        prefix.Hex.Fixed,
			}),
			2: field.NewString(&field.Spec{
				Length:      19,
				Description: "Primary Account Number",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.LL,
			}),
			3: field.NewComposite(&field.Spec{
				Length:      6,
				Description: "Processing Code",
				Pref:        prefix.ASCII.Fixed,
				Tag: &field.TagSpec{
					Sort: sort.StringsByInt,
				},
				Subfields: map[string]field.Field{
					"1": field.NewString(&field.Spec{
						Length:      2,
						Description: "Transaction Type",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
					"2": field.NewString(&field.Spec{
						Length:      2,
						Description: "From Account",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
					"3": field.NewString(&field.Spec{
						Length:      2,
						Description: "To Account",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
				},
			}),
			4: field.NewString(&field.Spec{
				Length:      12,
				Description: "Transaction Amount",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
				Pad:         padding.Left('0'),
			}),

			// this field will be ignored when packing and
			// unpacking, as bit 65 is a bitmap presence indicator
			65: field.NewString(&field.Spec{
				Length:      1,
				Description: "Settlement Code",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			// this is a field of the third bitmap
			130: field.NewString(&field.Spec{
				Length:      1,
				Description: "Additional Data",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
		},
	}

	b, err := specs.Builder.ExportJSON(spec)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(string(b))
}
