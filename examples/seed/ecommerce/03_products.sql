INSERT INTO products (id, sku, name, category, price, stock_qty) VALUES
    (1, 'KB-750', 'Mechanical Keyboard K750', 'Keyboards', 89.0, 33),
    (2, 'KB-210', 'Compact Keyboard C210', 'Keyboards', 49.5, 91),
    (3, 'MS-420', 'Ergo Mouse M420', 'Mice', 39.9, 155),
    (4, 'MS-115', 'Travel Mouse M115', 'Mice', 19.9, 48),
    (5, 'HP-880', 'Studio Headphones H880', 'Audio', 149.0, 11),
    (6, 'HP-330', 'Sport Earbuds E330', 'Audio', 59.0, 164),
    (7, 'MN-270', '27" 4K Monitor U270', 'Displays', 329.0, 58),
    (8, 'MN-240', '24" IPS Monitor I240', 'Displays', 179.0, 11),
    (9, 'ST-100', 'NVMe SSD 1TB', 'Storage', 94.5, 57),
    (10, 'ST-205', 'Portable SSD 2TB', 'Storage', 139.0, 14),
    (11, 'DK-050', 'Aluminium Laptop Stand', 'Desk', 45.0, 184),
    (12, 'DK-080', 'Monitor Arm Single', 'Desk', 79.0, 142),
    (13, 'WB-030', 'Desk Mat XL', 'Desk', 29.0, 78),
    (14, 'CB-011', 'USB-C Dock 11-port', 'Docks', 119.0, 139);
SELECT setval('products_id_seq', (SELECT MAX(id) FROM products));
