package main

import (
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/firestore"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	deployAWS()
	deployGCP()
}

func deployGCP() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		db, err := firestore.NewDatabase(ctx, "db", &firestore.DatabaseArgs{
			LocationId: pulumi.String("asia-southeast2"),
			Type:       pulumi.String("FIRESTORE_NATIVE"),
			Project:    pulumi.String("test-youtube-study-space"),
		})
		if err != nil {
			return err
		}
		ctx.Export("dbName", db.Name)
		
		return nil
	})
}
