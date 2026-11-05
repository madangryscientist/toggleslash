using System;
using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace FeatureSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class AddRulesetSnapshots : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.CreateTable(
                name: "ruleset_snapshots",
                columns: table => new
                {
                    environment_id = table.Column<long>(type: "INTEGER", nullable: false),
                    hash = table.Column<string>(type: "TEXT", nullable: false),
                    body = table.Column<byte[]>(type: "BLOB", nullable: false),
                    first_served_at = table.Column<DateTime>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_ruleset_snapshots", x => new { x.environment_id, x.hash });
                    table.ForeignKey(
                        name: "FK_ruleset_snapshots_environments_environment_id",
                        column: x => x.environment_id,
                        principalTable: "environments",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "ruleset_snapshots");
        }
    }
}
