package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccUserDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: unleashConfig + `
								data "unleash_user" "test" {
									id = 1
								}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify placeholder id attribute
					resource.TestCheckResourceAttr("data.unleash_user.test", "id", "1"),
					resource.TestCheckResourceAttr("data.unleash_user.test", "username", "admin"),
				),
			},
		},
	})
}
