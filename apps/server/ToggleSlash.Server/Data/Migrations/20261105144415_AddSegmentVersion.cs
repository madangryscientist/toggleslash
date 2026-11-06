using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace ToggleSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class AddSegmentVersion : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<long>(
                name: "version",
                table: "segments",
                type: "INTEGER",
                nullable: false,
                defaultValue: 1L);
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropColumn(
                name: "version",
                table: "segments");
        }
    }
}
