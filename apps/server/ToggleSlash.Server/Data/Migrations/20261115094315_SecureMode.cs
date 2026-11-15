using System;
using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace ToggleSlash.Server.Data.Migrations
{
    /// <inheritdoc />
    public partial class SecureMode : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<bool>(
                name: "secure_mode",
                table: "environments",
                type: "INTEGER",
                nullable: false,
                defaultValue: false);

            migrationBuilder.AddColumn<DateTime>(
                name: "signing_key_created_at",
                table: "environments",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<byte[]>(
                name: "signing_public_key",
                table: "environments",
                type: "BLOB",
                nullable: true);
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropColumn(
                name: "secure_mode",
                table: "environments");

            migrationBuilder.DropColumn(
                name: "signing_key_created_at",
                table: "environments");

            migrationBuilder.DropColumn(
                name: "signing_public_key",
                table: "environments");
        }
    }
}
