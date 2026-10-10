import unittest

from ci_go_packages import select_packages


class GoPackageSelectionTest(unittest.TestCase):
    def test_shards_cover_every_package_once_including_new_packages(self):
        module = "example.invalid/archive"
        packages = [module + suffix for suffix in (
            "/pkg/sqlite", "/pkg/sqlite/blob", "/pkg/sqlite/migrations",
            "/internal/api", "/pkg/sqlite_extra", "/future/new_service", "/ui",
        )]
        sqlite = select_packages(packages, module, "sqlite")
        other = select_packages(packages, module, "other")
        self.assertEqual(set(sqlite) | set(other), set(packages))
        self.assertEqual(set(sqlite) & set(other), set())
        self.assertEqual(sqlite, packages[:3])
        self.assertIn(module + "/pkg/sqlite_extra", other)

    def test_empty_partition_is_an_error(self):
        with self.assertRaises(ValueError):
            select_packages([], "example.invalid/archive", "sqlite")


if __name__ == "__main__":
    unittest.main()
