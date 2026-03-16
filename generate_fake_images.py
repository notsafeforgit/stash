import sqlite3
import hashlib

def insert_image(cur, img_id, title, phash, file_size=123456, width=1920, height=1080):
    # Insert image
    cur.execute("INSERT INTO images (id, title, created_at, updated_at) VALUES (?, ?, datetime('now'), datetime('now'))", (img_id, title))

    # Insert folder
    folder_id = img_id + 5000
    cur.execute("INSERT INTO folders (id, path, parent_folder_id, basename, zip_file_id, mod_time, created_at, updated_at) VALUES (?, ?, NULL, ?, NULL, datetime('now'), datetime('now'), datetime('now'))", (folder_id, f"/images/{title}", "images"))

    # Insert file
    file_id = img_id + 1000
    cur.execute("INSERT INTO files (id, parent_folder_id, basename, size, mod_time, created_at, updated_at) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'), datetime('now'))", (file_id, folder_id, f"{title}.jpg", file_size))

    # Insert image file link
    cur.execute("INSERT INTO images_files (image_id, file_id, `primary`) VALUES (?, ?, 1)", (img_id, file_id))

    # Insert fingerprints
    cur.execute("INSERT INTO files_fingerprints (file_id, fingerprint, type) VALUES (?, ?, 'phash')", (file_id, phash))

    # MD5 Fingerprint just so it has something
    md5 = hashlib.md5(title.encode()).hexdigest()
    cur.execute("INSERT INTO files_fingerprints (file_id, fingerprint, type) VALUES (?, ?, 'md5')", (file_id, md5))

    # Insert Video/Image metadata (resolutions)
    cur.execute("INSERT INTO image_files (file_id, width, height, format) VALUES (?, ?, ?, 'jpeg')", (file_id, width, height))

def main():
    conn = sqlite3.connect("/app/.local/stash-go.sqlite")
    cur = conn.cursor()

    # Clear out any existing data to avoid conflicts on re-run
    cur.execute("DELETE FROM images")
    cur.execute("DELETE FROM folders")
    cur.execute("DELETE FROM files")
    cur.execute("DELETE FROM images_files")
    cur.execute("DELETE FROM files_fingerprints")
    cur.execute("DELETE FROM image_files")

    # Create a couple exact duplicates (distance 0). Using valid 16-char hex phashes
    insert_image(cur, 1001, "Duplicate Image A", "1a2b3c4d5e6f7a8b")
    insert_image(cur, 1002, "Duplicate Image B", "1a2b3c4d5e6f7a8b", file_size=500000, width=800, height=600)

    # Create near duplicates (distance 1-3)
    insert_image(cur, 1003, "Near Dupe A", "1111111111111111")
    insert_image(cur, 1004, "Near Dupe B", "1111111111111112") # Distance 1 bit off

    conn.commit()
    conn.close()
    print("Inserted mock images!")

if __name__ == "__main__":
    main()
