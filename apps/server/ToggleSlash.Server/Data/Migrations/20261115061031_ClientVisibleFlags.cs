using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace ToggleSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class ClientVisibleFlags : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<bool>(
                name: "client_visible",
                table: "flags",
                type: "INTEGER",
                nullable: false,
                defaultValue: false);
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropColumn(
                name: "client_visible",
                table: "flags");
        }
    }
}
