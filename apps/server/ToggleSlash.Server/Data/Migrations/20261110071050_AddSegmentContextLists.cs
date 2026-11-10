using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace ToggleSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class AddSegmentContextLists : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<string>(
                name: "excluded_contexts",
                table: "segments",
                type: "TEXT",
                nullable: false,
                defaultValue: "[]");

            migrationBuilder.AddColumn<string>(
                name: "included_contexts",
                table: "segments",
                type: "TEXT",
                nullable: false,
                defaultValue: "[]");
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropColumn(
                name: "excluded_contexts",
                table: "segments");

            migrationBuilder.DropColumn(
                name: "included_contexts",
                table: "segments");
        }
    }
}
