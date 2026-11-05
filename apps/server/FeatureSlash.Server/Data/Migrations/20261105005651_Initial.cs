using System;
using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace FeatureSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class Initial : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.CreateTable(
                name: "projects",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    key = table.Column<string>(type: "TEXT", nullable: false),
                    name = table.Column<string>(type: "TEXT", nullable: false),
                    created_at = table.Column<DateTime>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_projects", x => x.id);
                });

            migrationBuilder.CreateTable(
                name: "environments",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    project_id = table.Column<long>(type: "INTEGER", nullable: false),
                    key = table.Column<string>(type: "TEXT", nullable: false),
                    name = table.Column<string>(type: "TEXT", nullable: false),
                    created_at = table.Column<DateTime>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_environments", x => x.id);
                    table.ForeignKey(
                        name: "FK_environments_projects_project_id",
                        column: x => x.project_id,
                        principalTable: "projects",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });

            migrationBuilder.CreateTable(
                name: "flags",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    project_id = table.Column<long>(type: "INTEGER", nullable: false),
                    key = table.Column<string>(type: "TEXT", nullable: false),
                    name = table.Column<string>(type: "TEXT", nullable: false),
                    description = table.Column<string>(type: "TEXT", nullable: false),
                    variations = table.Column<string>(type: "TEXT", nullable: false),
                    salt = table.Column<string>(type: "TEXT", nullable: false),
                    temporary = table.Column<bool>(type: "INTEGER", nullable: false),
                    owner = table.Column<string>(type: "TEXT", nullable: true),
                    tags = table.Column<string>(type: "TEXT", nullable: false),
                    created_at = table.Column<DateTime>(type: "TEXT", nullable: false),
                    archived_at = table.Column<DateTime>(type: "TEXT", nullable: true)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_flags", x => x.id);
                    table.ForeignKey(
                        name: "FK_flags_projects_project_id",
                        column: x => x.project_id,
                        principalTable: "projects",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });

            migrationBuilder.CreateTable(
                name: "segments",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    project_id = table.Column<long>(type: "INTEGER", nullable: false),
                    environment_id = table.Column<long>(type: "INTEGER", nullable: false),
                    key = table.Column<string>(type: "TEXT", nullable: false),
                    salt = table.Column<string>(type: "TEXT", nullable: false),
                    included = table.Column<string>(type: "TEXT", nullable: false),
                    excluded = table.Column<string>(type: "TEXT", nullable: false),
                    rules = table.Column<string>(type: "TEXT", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_segments", x => x.id);
                    table.ForeignKey(
                        name: "FK_segments_environments_environment_id",
                        column: x => x.environment_id,
                        principalTable: "environments",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                    table.ForeignKey(
                        name: "FK_segments_projects_project_id",
                        column: x => x.project_id,
                        principalTable: "projects",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });

            migrationBuilder.CreateTable(
                name: "flag_environment_configs",
                columns: table => new
                {
                    id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    flag_id = table.Column<long>(type: "INTEGER", nullable: false),
                    environment_id = table.Column<long>(type: "INTEGER", nullable: false),
                    on = table.Column<bool>(type: "INTEGER", nullable: false),
                    off_variation = table.Column<int>(type: "INTEGER", nullable: true),
                    targets = table.Column<string>(type: "TEXT", nullable: false),
                    prerequisites = table.Column<string>(type: "TEXT", nullable: false),
                    rules = table.Column<string>(type: "TEXT", nullable: false),
                    fallthrough = table.Column<string>(type: "TEXT", nullable: false),
                    version = table.Column<long>(type: "INTEGER", nullable: false)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_flag_environment_configs", x => x.id);
                    table.ForeignKey(
                        name: "FK_flag_environment_configs_environments_environment_id",
                        column: x => x.environment_id,
                        principalTable: "environments",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                    table.ForeignKey(
                        name: "FK_flag_environment_configs_flags_flag_id",
                        column: x => x.flag_id,
                        principalTable: "flags",
                        principalColumn: "id",
                        onDelete: ReferentialAction.Cascade);
                });

            migrationBuilder.CreateIndex(
                name: "IX_environments_project_id_key",
                table: "environments",
                columns: new[] { "project_id", "key" },
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_flag_environment_configs_environment_id",
                table: "flag_environment_configs",
                column: "environment_id");

            migrationBuilder.CreateIndex(
                name: "IX_flag_environment_configs_flag_id_environment_id",
                table: "flag_environment_configs",
                columns: new[] { "flag_id", "environment_id" },
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_flags_project_id_key",
                table: "flags",
                columns: new[] { "project_id", "key" },
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_projects_key",
                table: "projects",
                column: "key",
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_segments_environment_id_key",
                table: "segments",
                columns: new[] { "environment_id", "key" },
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_segments_project_id",
                table: "segments",
                column: "project_id");
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "flag_environment_configs");

            migrationBuilder.DropTable(
                name: "segments");

            migrationBuilder.DropTable(
                name: "flags");

            migrationBuilder.DropTable(
                name: "environments");

            migrationBuilder.DropTable(
                name: "projects");
        }
    }
}
