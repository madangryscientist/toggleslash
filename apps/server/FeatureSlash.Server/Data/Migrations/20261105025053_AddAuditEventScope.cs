using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace FeatureSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class AddAuditEventScope : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<string>(
                name: "environment_key",
                table: "audit_events",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<string>(
                name: "flag_key",
                table: "audit_events",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<string>(
                name: "project_key",
                table: "audit_events",
                type: "TEXT",
                nullable: false,
                defaultValue: "");

            migrationBuilder.CreateIndex(
                name: "IX_audit_events_project_key_flag_key_id",
                table: "audit_events",
                columns: new[] { "project_key", "flag_key", "id" });

            migrationBuilder.CreateIndex(
                name: "IX_audit_events_project_key_id",
                table: "audit_events",
                columns: new[] { "project_key", "id" });
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropIndex(
                name: "IX_audit_events_project_key_flag_key_id",
                table: "audit_events");

            migrationBuilder.DropIndex(
                name: "IX_audit_events_project_key_id",
                table: "audit_events");

            migrationBuilder.DropColumn(
                name: "environment_key",
                table: "audit_events");

            migrationBuilder.DropColumn(
                name: "flag_key",
                table: "audit_events");

            migrationBuilder.DropColumn(
                name: "project_key",
                table: "audit_events");
        }
    }
}
