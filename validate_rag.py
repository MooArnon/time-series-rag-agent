import os
import random
import numpy as np
import pandas as pd
import matplotlib.pyplot as plt
from sklearn.decomposition import PCA
from sqlalchemy import create_engine, text
import ast

from config.config import config

# 1. Setup Connection
HOST = config["DB_HOST"]
PORT = config["DB_PORT"]
NAME = config["DB_NAME"]
USER = config["DB_USER"]
PASS = config["DB_PASSWORD"]


# SSL Mode Disable for local testing
DATABASE_URI = f"postgresql://{USER}:{PASS}@{HOST}:{PORT}/{NAME}?sslmode=disable"
engine = create_engine(DATABASE_URI)

# --- CONFIGURATION ---
TABLE_NAME = "market_pattern"  # REPLACE with your actual table name!
VECTOR_COL = "embedding"        # REPLACE with your vector column name
TOP_K = 10
NOISE_SAMPLES = 50              # Random points to show contrast in the plot

def parse_vector(vec_str):
    """Parses pgvector string '[0.1, 0.2, ...]' into a numpy array."""
    if isinstance(vec_str, list):
        return np.array(vec_str)
    # If it comes back as a string from DB
    return np.array(ast.literal_eval(vec_str))

def validate_vector_logic():
    print(f"--- 1. Fetching a Random Target Point from '{TABLE_NAME}' ---")
    
    with engine.connect() as conn:
        # 1. Update the ID column name here (e.g., change 'id' to 'dt')
        ID_COL = "time"  # <--- CHANGE THIS to your actual column name

        query_target = text(f"""
            SELECT * FROM {TABLE_NAME} 
            WHERE {VECTOR_COL} IS NOT NULL 
            ORDER BY RANDOM() 
            LIMIT 1
        """)
        target_row = conn.execute(query_target).mappings().one()
        
        # 2. Update how we extract the ID
        target_id = target_row[ID_COL] 
        target_vec_str = target_row[VECTOR_COL]
        target_vec = parse_vector(target_vec_str)
        
        print(f"-> Selected Target {ID_COL}: {target_id}")
        
        print(f"--- 2. Searching Top {TOP_K} Nearest Neighbors ---")
        query_neighbors = text(f"""
            SELECT *, {VECTOR_COL} <=> :target_vec_str as distance
            FROM {TABLE_NAME}
            WHERE {VECTOR_COL} IS NOT NULL
            AND {ID_COL} != :target_id  -- <--- Exclude using the correct column
            ORDER BY distance ASC
            LIMIT :top_k
        """)
        
        neighbors = conn.execute(query_neighbors, {
            "target_vec_str": str(target_vec.tolist()),
            "target_id": target_id,
            "top_k": TOP_K
        }).mappings().all()
        
        print(f"-> Found {len(neighbors)} neighbors.")

        # C. Fetch Random Noise (For visual context)
        print(f"--- 3. Fetching {NOISE_SAMPLES} Random Noise Points ---")
        query_noise = text(f"""
            SELECT * FROM {TABLE_NAME}
            WHERE {VECTOR_COL} IS NOT NULL
            AND time != :target_id
            ORDER BY RANDOM()
            LIMIT :noise_count
        """)
        noise_rows = conn.execute(query_noise, {
            "target_id": target_id,
            "noise_count": NOISE_SAMPLES
        }).mappings().all()

    # --- DATA PREP FOR PLOTTING ---
    print("--- 4. Preparing Plot ---")
    
    # Collect all vectors
    vectors = [target_vec]
    labels = ["Target"]
    colors = ["red"]
    sizes = [200]
    markers = ["*"]
    
    # Add Neighbors
    for n in neighbors:
        vectors.append(parse_vector(n[VECTOR_COL]))
        labels.append(f"Neighbor (d={n['distance']:.3f})")
        colors.append("blue")
        sizes.append(100)
        markers.append("o")
        
    # Add Noise
    for n in noise_rows:
        vectors.append(parse_vector(n[VECTOR_COL]))
        labels.append("Noise")
        colors.append("lightgrey")
        sizes.append(30)
        markers.append(".")

    # Convert to Matrix
    X = np.array(vectors)

    # --- PCA (Dimensionality Reduction) ---
    # Reduce 1536 dimensions -> 2 dimensions
    pca = PCA(n_components=2)
    X_2d = pca.fit_transform(X)

    # --- PLOTTING ---
    plt.figure(figsize=(10, 7))
    
    # Plot Noise first (background)
    plt.scatter(X_2d[TOP_K+1:, 0], X_2d[TOP_K+1:, 1], c='lightgrey', alpha=0.5, label='Random Noise')
    
    # Plot Neighbors
    plt.scatter(X_2d[1:TOP_K+1, 0], X_2d[1:TOP_K+1, 1], c='blue', alpha=0.7, label='Top K Matches')
    
    # Plot Target (last to be on top)
    plt.scatter(X_2d[0, 0], X_2d[0, 1], c='red', s=200, marker='*', label='Query Target')

    plt.title(f"RAG Logic Validation: PCA Projection\nTarget ID: {target_id}")
    plt.xlabel("Principal Component 1")
    plt.ylabel("Principal Component 2")
    plt.legend()
    plt.grid(True, linestyle='--', alpha=0.3)
    
    # Save plot
    output_file = "rag_validation_plot.png"
    plt.savefig(output_file)
    print(f"\n✅ Success! Plot saved to: {output_file}")
    print("Check this image. If logic works, Blue dots should be closer to the Red star than Grey dots.")

if __name__ == "__main__":
    try:
        validate_vector_logic()
    except Exception as e:
        print(f"Error: {e}")
        print("\nNOTE: Ensure your TABLE_NAME and VECTOR_COL variables match your DB schema.")