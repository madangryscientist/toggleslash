using System;
using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace FeatureSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class AddAdminTokens : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.CreateTable(
                name: "admin_tokens",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    project_id = table.Column<long>(type: "INTEGER", nullable: false),
                    name = table.Column<string>(type: "TEXT", nullable: false),
                    permission = table.Column<string>(type: "TEXT", nullable: false),
                    hash = table.Column<string>(type: "TEXT", nullable: false),
                    prefix = table.Column<string>(type: "TEXT", nullable: false),
                    created_at = table.Column<DateTime>(type: "TEXT", nullable: false),
                    expires_at = table.Column<DateTime>(type: "TEXT", nullable: false),
                    revoked_at = table.Column<DateTime>(type: "TEXT", nullable: true)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_admin_tokens", x => x.id);
                    table.CheckConstraint("ck_admin_tokens_permission", "permission IN ('read', 'write')");
                    table.ForeignKey(
                        name: "FK_admin_tokens_projects_project_id",
                        column: x => x.project_id,
                        principalTable: "projects",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });

            migrationBuilder.CreateIndex(
                name: "IX_admin_tokens_hash",
                table: "admin_tokens",
                column: "hash",
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_admin_tokens_project_id",
                table: "admin_tokens",
                column: "project_id");
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "admin_tokens");
        }
    }
}
