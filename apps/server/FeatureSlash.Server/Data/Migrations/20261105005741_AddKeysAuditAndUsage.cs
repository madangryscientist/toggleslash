using System;
using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace FeatureSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class AddKeysAuditAndUsage : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.CreateTable(
                name: "api_keys",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    environment_id = table.Column<long>(type: "INTEGER", nullable: false),
                    kind = table.Column<string>(type: "TEXT", nullable: false),
                    name = table.Column<string>(type: "TEXT", nullable: false),
                    hash = table.Column<string>(type: "TEXT", nullable: false),
                    prefix = table.Column<string>(type: "TEXT", nullable: false),
                    created_at = table.Column<DateTime>(type: "TEXT", nullable: false),
                    revoked_at = table.Column<DateTime>(type: "TEXT", nullable: true)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_api_keys", x => x.id);
                    table.CheckConstraint("ck_api_keys_kind", "kind IN ('server', 'client')");
                    table.ForeignKey(
                        name: "FK_api_keys_environments_environment_id",
                        column: x => x.environment_id,
                        principalTable: "environments",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });

            migrationBuilder.CreateTable(
                name: "audit_events",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    occurred_at = table.Column<DateTime>(type: "TEXT", nullable: false),
                    actor = table.Column<string>(type: "TEXT", nullable: false),
                    entity_type = table.Column<string>(type: "TEXT", nullable: false),
                    entity_id = table.Column<string>(type: "TEXT", nullable: false),
                    action = table.Column<string>(type: "TEXT", nullable: false),
                    before = table.Column<string>(type: "TEXT", nullable: true),
                    after = table.Column<string>(type: "TEXT", nullable: true)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_audit_events", x => x.id);
                });

            migrationBuilder.CreateTable(
                name: "flag_usage_daily",
                columns: table => new
                {
                    environment_id = table.Column<long>(type: "INTEGER", nullable: false),
                    flag_id = table.Column<long>(type: "INTEGER", nullable: false),
                    variation = table.Column<int>(type: "INTEGER", nullable: false),
                    day = table.Column<DateOnly>(type: "TEXT", nullable: false),
                    count = table.Column<long>(type: "INTEGER", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_flag_usage_daily", x => new { x.environment_id, x.flag_id, x.variation, x.day });
                    table.ForeignKey(
                        name: "FK_flag_usage_daily_environments_environment_id",
                        column: x => x.environment_id,
                        principalTable: "environments",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                    table.ForeignKey(
                        name: "FK_flag_usage_daily_flags_flag_id",
                        column: x => x.flag_id,
                        principalTable: "flags",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });

            migrationBuilder.CreateIndex(
                name: "IX_api_keys_environment_id",
                table: "api_keys",
                column: "environment_id");

            migrationBuilder.CreateIndex(
                name: "IX_api_keys_hash",
                table: "api_keys",
                column: "hash",
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_flag_usage_daily_flag_id",
                table: "flag_usage_daily",
                column: "flag_id");
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "api_keys");

            migrationBuilder.DropTable(
                name: "audit_events");

            migrationBuilder.DropTable(
                name: "flag_usage_daily");
        }
    }
}
